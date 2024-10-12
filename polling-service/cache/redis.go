package cache

import (
	"polling-service/pkg/models"

	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
)

type RedisClient struct {
	Client *redis.Client
	logger *slog.Logger
}

func NewRedisClient(logger *slog.Logger, options *redis.Options) (*RedisClient, error) {
	rc := &RedisClient{
		Client: redis.NewClient(options),
		logger: logger,
	}
	logger.Info("connecting to redis...")

	// Attempting Redis connection, log any potential error
	_, err := rc.Client.Ping(context.Background()).Result()
	if err != nil {
		logger.Error("failed to connect to Redis", slog.String("error", err.Error()))
		return nil, err
	}

	logger.Info("successfully connected to Redis")
	return rc, nil
}

//-------------------------------------
// Redis Caching Functions
//-------------------------------------

type RedisStorer interface {
	StoreTrainInfo(id string, info map[string]string, expiration time.Duration) error
	StorePreviousCallingPoints(id string, mp *models.Service, expiration time.Duration) error
	StoreSubsequentCallingPoints(id string, mp *models.Service, expiration time.Duration) error
}

func (rc *RedisClient) StoreTrainInfo(id string, info map[string]string, expiration time.Duration) error {

	ctx := context.Background()

	key := fmt.Sprintf("train:%s:info", id)
	_, err := rc.Client.HSet(ctx, key, info).Result()
	if err != nil {
		rc.logger.Error("failed to store train info", slog.String("key", key), slog.String("error", err.Error()))
		return fmt.Errorf("failed to store service info: %w", err)
	}

	// Setting expiration
	if err := rc.Client.Expire(ctx, key, expiration).Err(); err != nil {
		rc.logger.Error("failed to set expiration", slog.String("key", key), slog.String("error", err.Error()))
		return fmt.Errorf("failed to store expiration for service info: %w", err)
	}

	return nil
}

func (rc *RedisClient) StorePreviousCallingPoints(id string, mp *models.Service, expiration time.Duration) error {
	ctx := context.Background()

	// Unique keys for previous and subsequent calling points
	previousKey := fmt.Sprintf("train:%s:previous", id)

	jsonPrevious, err := json.Marshal(mp.PreviousCallingPoints)
	if err != nil {
		return fmt.Errorf("failed to marshall previous calling points: %w", err)
	}

	_, err = rc.Client.JSONSet(ctx, previousKey, "$", jsonPrevious).Result()
	if err != nil {
		return fmt.Errorf("failed to store previous calling points: %w", err)
	}

	if err = rc.Client.Expire(ctx, previousKey, expiration).Err(); err != nil {
		return fmt.Errorf("failed to store expiration for calling points: %w", err)
	}
	return nil
}

func (rc *RedisClient) StoreSubsequentCallingPoints(id string, mp *models.Service, expiration time.Duration) error {
	ctx := context.Background()

	// Unique keys for previous and subsequent calling points
	subsequentKey := fmt.Sprintf("train:%s:subsequent", id)

	jsonPrevious, err := json.Marshal(mp.SubsequentCallingPoints)
	if err != nil {
		return fmt.Errorf("failed to marshall subsequent calling points: %w", err)
	}

	_, err = rc.Client.JSONSet(ctx, subsequentKey, "$", jsonPrevious).Result()
	if err != nil {
		return fmt.Errorf("failed to store previous calling points: %w", err)
	}

	if err = rc.Client.Expire(ctx, subsequentKey, expiration).Err(); err != nil {
		return fmt.Errorf("failed to store expiration for calling points: %w", err)
	}
	return nil
}

//----------------------------------------------
// Redis Cache Handling & Detecting Changes To Send To Channels For SSE
//----------------------------------------------

type RedisCacheHandler interface {
	FetchHSetFromCache(id string) (map[string]string, error)
	FetchJSONFromCache(id string) ([]byte, error)
	CheckIDExists(id string) (int64, error)
}

func (rc *RedisClient) FetchHSetFromCache(id string) (map[string]string, error) {
	ctx := context.Background()
	info, err := rc.Client.HGetAll(ctx, id).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch service info: %w", err)
	}
	return info, nil
}

func (rc *RedisClient) FetchJSONFromCache(id string) ([]byte, error) {
	ctx := context.Background()
	info, err := rc.Client.JSONGet(ctx, id).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch service info: %w", err)
	}
	return []byte(info), nil
}

func (rc *RedisClient) CheckIDExists(id string) (int64, error) {
	ctx := context.Background()
	exists, err := rc.Client.Exists(ctx, id).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to check if key exists: %w", err)
	}
	return exists, nil
}

func CheckKeyStructure(id, want string) (string, error) {
	key := strings.Split(id, ":")
	switch {
	case len(key) == 3:
		return id, nil
	case len(key) == 2:
		return fmt.Sprintf("%s:%s", id, want), nil
	case len(key) == 1:
		return fmt.Sprintf("train:%s:%s", id, want), nil
	default:
		return id, nil
	}
}

func CompareTrainInfoHSET(info, trainInfo map[string]string) bool {

	// Comparison Logic
	for key, expectedValue := range info {
		if key == "StationName" {
			continue //Skipping StationName as this is only the anchor for the request data and doesn't matter if it changes
		}
		if trainInfo[key] != expectedValue {
			// Return false if any field does not match
			return true
		}
	}
	return false
}

func CompareCallingPointJSON(info *[]models.CallingPoint, callingPoints []byte) (bool, error) {

	var tempCallingPoints []models.CallingPoint

	err := json.Unmarshal(callingPoints, &tempCallingPoints)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal calling points: %w", err)
	}
	// Return true if there is a change (i.e., the two are not equal)
	return !reflect.DeepEqual(tempCallingPoints, *info), nil
}

// CheckHSET uses the RedisCacheHandler to invoke the checking methods on a train item in the cache
// It checks the key structure expecting "train:12345:info" and if it exists
// If it exists, it then fetches the item from Redis Cache and calls CompareTrainInfoHSET
// Returning False will signal that no changes are required - Returning True will signal that a change needs to be pushed
func CheckHSET(ch RedisCacheHandler, info map[string]string, id string, tag string) (bool, error) {
	key, err := CheckKeyStructure(id, tag)
	if err != nil {
		return false, err
	}

	exists, err := ch.CheckIDExists(key)
	if err != nil {
		return false, fmt.Errorf("failed to check if key exists: %w", err)
	}
	if exists == 0 {
		return false, nil
	}
	trainInfo, err := ch.FetchHSetFromCache(key)
	if err != nil {
		return false, fmt.Errorf("failed to fetch service info: %w", err)
	}
	return CompareTrainInfoHSET(trainInfo, info), nil
}

func checkHSET(ctx context.Context, wg *sync.WaitGroup, rh RedisHandler, info map[string]string, id string, tag string, errCh chan<- error) {
	defer wg.Done()
	check, err := CheckHSET(rh, info, id, tag)
	if err != nil {
		errCh <- err
		return
	}
	if check {
		key := strings.Split(id, ":")
		// Publish only if the check was successful
		if err := rh.RPublishInfo(ctx, tag, key[0], info); err != nil {
			errCh <- err
		}
	}
}

func CheckJSON(ch RedisCacheHandler, info *[]models.CallingPoint, id string, tag string) (bool, error) {
	key, err := CheckKeyStructure(id, tag)
	if err != nil {
		return false, err
	}

	exists, err := ch.CheckIDExists(key)
	if err != nil {
		return false, fmt.Errorf("failed to check if key exists: %w", err)
	}
	if exists == 0 {
		return false, nil
	}
	trainInfo, err := ch.FetchJSONFromCache(key)
	if err != nil {
		return false, fmt.Errorf("failed to fetch service info: %w", err)
	}
	return CompareCallingPointJSON(info, trainInfo)
}

func checkJSON(ctx context.Context, wg *sync.WaitGroup, rh RedisHandler, info *[]models.CallingPoint, id string, tag string, errCh chan<- error) {
	defer wg.Done()
	check, err := CheckJSON(rh, info, id, tag)
	if err != nil {
		errCh <- err
		return // Early return on error to avoid further processing
	}
	if check {
		key := strings.Split(id, ":")
		// Publish only if the check was successful
		if err := rh.RPublishCP(ctx, tag, key[0], info); err != nil {
			errCh <- err
		}
	}
}

func checkNew(ctx context.Context, wg *sync.WaitGroup, rh RedisHandler, id string, value *models.ProcessedService, errCh chan<- error) {
	defer wg.Done()
	// Check if the key exists in Redis
	check, err := rh.CheckIDExists(fmt.Sprintf("train:%s:info", id))
	if err != nil {
		errCh <- fmt.Errorf("failed to check if ID exists for %s: %w", id, err)
		return
	}
	if check == 0 {
		// The key does not exist; this is considered new data
		// Publish to redis...
		err := rh.RPublishFull(ctx, "new", value.Service.ServiceID, value)
		if err != nil {
			errCh <- err
			return
		}
	}
}

//----------------------------------------------
// Caching Data
//----------------------------------------------

type RedisHandler interface {
	RedisCacheHandler
	RedisStorer
	RedisPublisher
}

// RedisCacheData takes a RedisHandler which combines RedisCacheHandler, RedisPublisher interfaces and RedisStorer interface. It also takes the mapped data which
// Is polled from the SOAP API and processed by the train_data service
// CacheData loops through the mapped data values and compares the data with what is already cached, if there are any changes
// It will send the changes to be published with a tag and id attached
// After which the new mapped data will be cached in Redis
func RedisCacheData(rh RedisHandler, mp *models.MappedData, logger *slog.Logger) error {
	var wg sync.WaitGroup

	intialGoRoutines := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background()) // <- parent context to propagate to child in order to control processing
	defer cancel()                                          // Ensure context is cancelled to release resources

	// Error channel to capture errors from all goroutines
	errCh := make(chan error, 1) //TODO review size of error channel and if we want to cancel - may need error types

	// Launch a separate goroutine to collect errors
	go func() {
		for err := range errCh {
			if err != nil {
				log.Printf("error: %s", err)
				cancel() // Cancel context if any error occurs
			}
		}
	}()

	for _, value := range mp.Map {
		value := value // Capture value to avoid closure issues
		wg.Add(1)
		go func() {
			defer wg.Done()

			info := map[string]string{
				"StationName":         value.StationName,
				"OriginLocation":      value.Service.Origin.Location,
				"OriginCRS":           value.Service.Origin.CRS,
				"DestinationLocation": value.Service.Destination.Location,
				"DestinationCRS":      value.Service.Destination.CRS,
			}

			// Checks
			checkWg := sync.WaitGroup{}
			checkWg.Add(4)
			go checkJSON(ctx, &checkWg, rh, &value.Service.PreviousCallingPoints, value.Service.ServiceID, "previous", errCh)
			go checkJSON(ctx, &checkWg, rh, &value.Service.SubsequentCallingPoints, value.Service.ServiceID, "subsequent", errCh)
			go checkHSET(ctx, &checkWg, rh, info, value.Service.ServiceID, "info", errCh)
			go checkNew(ctx, &checkWg, rh, value.Service.ServiceID, value, errCh)
			checkWg.Wait()

			// Return early if context cancellation on checks
			if ctx.Err() != nil {
				logger.Info("Goroutine canceled for: ", slog.String("service", value.Service.ServiceID))
				return
			}

			// Storing the data

			storeWg := sync.WaitGroup{}
			storeWg.Add(3)

			// TODO Refactor below to be more clean and efficient

			go func() {
				defer storeWg.Done()
				err := rh.StoreTrainInfo(value.Service.ServiceID, info, value.Expiration)
				if err != nil {
					errCh <- fmt.Errorf("failed to store service info: %w", err)
				}
			}()

			go func() {
				defer storeWg.Done()
				err := rh.StorePreviousCallingPoints(value.Service.ServiceID, value.Service, value.Expiration)
				if err != nil {
					errCh <- fmt.Errorf("failed to store previous calling points: %w", err)
				}
			}()

			go func() {
				defer storeWg.Done()
				err := rh.StoreSubsequentCallingPoints(value.Service.ServiceID, value.Service, value.Expiration)
				if err != nil {
					errCh <- fmt.Errorf("failed to store subsequent calling points: %w", err)
				}
			}()

			storeWg.Wait() // Wait for all storage operations to complete

		}()
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errCh) // Close the error channel to terminate the error collection goroutine

	endGoRoutines := runtime.NumGoroutine()

	if endGoRoutines-intialGoRoutines > 10 {
		logger.Warn("significant change in start and end number of go-routines", slog.Int("start", intialGoRoutines), slog.Int("end", endGoRoutines))
	}

	// Return nil if context was not cancelled due to errors
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return nil
}
