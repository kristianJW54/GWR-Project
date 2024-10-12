package tests

import (
	"GWR_Project/cmd/polling-service/cache"
	"GWR_Project/cmd/polling-service/pkg/models"
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"strings"
	"testing"
)

//Test Redis connection

//Test Redis store of data

//Test Redis JSON Compare

//Test Redis pub/sub ....

//Test Redis Client Connection store

func TestRedisLive(t *testing.T) {

	t.Run("testing connection to redis", func(t *testing.T) {
		client := &cache.RedisClient{}
		rc := setupRedisClient(t, client)

		ctx := context.Background()
		out := rc.Client.Ping(ctx).String()
		want := "ping: PONG"
		if out != want {
			t.Fatalf("got %s, want %s", out, want)
		}
	})

	t.Run("testing comparing HSET", func(t *testing.T) {
		client := &cache.RedisClient{}
		rc := setupRedisClient(t, client)

		key, err := FetchRandomKey(rc.Client)
		if err != nil {
			t.Fatal(err)
		}

		t.Log(key)

		exists, err := rc.CheckIDExists(fmt.Sprintf("%s:info", key))
		if err != nil {
			t.Fatal(err)
		}

		if exists == 0 {
			t.Fatal("key should exist")
		}

		fromCache, err := rc.FetchHSetFromCache(fmt.Sprintf("%s:info", key))
		if err != nil {
			return
		}

		got := cache.CompareTrainInfoHSET(fromCache, fromCache)

		if got != false {
			t.Fatalf("got %t, want false", got)
		}

	})

}

func TestCallingPointComparison(t *testing.T) {
	client := &cache.RedisClient{}
	rc := setupRedisClient(t, client) // Make sure this function sets up Redis properly

	tests := []struct {
		name   string
		change bool
		want   bool
		id     string
	}{
		{
			name:   "Test Case 1",
			change: false,
			want:   false,
			id:     "previous",
		},
		{
			name:   "Test Case 2",
			change: true,
			want:   true,
			id:     "previous",
		},
		{
			name:   "Test Case 3",
			change: true,
			want:   true,
			id:     "subsequent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the comparison function with dependencies
			comparator := NewJSONCompare(t, rc, tt.change, tt.id)

			// Perform the comparison using the middleware-style function
			match, err := comparator(cache.CompareCallingPointJSON)
			if err != nil {
				t.Fatal(err)
			}

			if match != tt.want {
				t.Fatalf("got %v, want %v", match, tt.want)
			}
		})
	}
}

//-----------SETUP & HELPERS-----------------//

func NewRedisClient(rc *cache.RedisClient) (*cache.RedisClient, error) {
	rc.Client = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,
	})
	return rc, nil
}

func CloseClient(client *cache.RedisClient) error {
	return client.Client.Close()
}

func FetchRandomKey(client *redis.Client) (string, error) {
	ctx := context.Background()

	// Use SCAN to find keys matching the pattern "service:*"
	iter := client.Scan(ctx, 0, "train:*", 1).Iterator()

	for iter.Next(ctx) {
		// Return the first key found matching the pattern
		id := iter.Val()
		parts := strings.Split(id, ":")
		return fmt.Sprintf("%s:%s", parts[0], parts[1]), nil
	}

	// Handle any error encountered while iterating
	if err := iter.Err(); err != nil {
		return "", fmt.Errorf("could not fetch random service key: %v", err)
	}

	return "", fmt.Errorf("no keys found matching pattern 'service:*'")
}

// Helper function to initialize Redis client and fetch a random key
func setupRedisClient(t *testing.T, client *cache.RedisClient) *cache.RedisClient {
	t.Helper() // This marks this function as a helper

	rc, err := NewRedisClient(client)
	if err != nil {
		t.Fatal(err)
	}

	// Defer closing the Redis client
	t.Cleanup(func() {
		if err := CloseClient(rc); err != nil {
			t.Errorf("failed to close Redis client: %v", err)
		}
	})

	return rc
}

func NewJSONCompare(
	t *testing.T,
	rc *cache.RedisClient,
	change bool,
	id string,
) func(compareFunc func(info *[]models.CallingPoint, fromCache []byte) (bool, error)) (bool, error) {
	t.Helper()
	key, err := FetchRandomKey(rc.Client)
	if err != nil {
		t.Fatal(err)
	}

	exists, err := rc.CheckIDExists(fmt.Sprintf("%s:%s", key, id))
	if err != nil {
		t.Fatal(err)
	}
	if exists == 0 {
		t.Fatal("key should exist")
	}

	cachedData, err := rc.FetchJSONFromCache(fmt.Sprintf("%s:%s", key, id))
	if err != nil {
		t.Fatal(err)
	}

	// Unmarshal cachedData into a non-pointer slice
	var tempCallingPoints []models.CallingPoint
	err = json.Unmarshal(cachedData, &tempCallingPoints)
	if err != nil {
		t.Fatal(err)
	}

	// Use a slice pointer to hold the data
	info := &tempCallingPoints

	return func(compareFunc func(info *[]models.CallingPoint, fromCache []byte) (bool, error)) (bool, error) {

		if change {
			(*info)[0].LocationName = "Changed Location"
		}
		return compareFunc(info, cachedData)
	}
}
