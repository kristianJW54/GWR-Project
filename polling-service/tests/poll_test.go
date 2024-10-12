package tests

import (
	"GWR_Project/cmd/polling-service/pkg/models"
	"GWR_Project/cmd/polling-service/service"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TODO maybe move test to the location of the module...?
// TODO create table tests to test different timings and limits etc...
// TODO create test for processing mock data within the ticker
// TODO create test for monitoring go routines count throughout the entire lifetime of a single tick

func TestPollDataTicker(t *testing.T) {
	t.Run("testing ticker in poll data function with mock client", func(t *testing.T) {

		pc := GenMockTicker(t, 12*time.Second, 2, 2*time.Second)
		client := &MockRedisHandler{}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		// Call PollData
		go service.PollData(ctx, "token", "url", pc.Stations, pc.Requester, pc.PollMonitor, client)
		time.Sleep(11 * time.Second)
		wg.Wait()

	})

	t.Run("testing manual context cancellation on poll data", func(t *testing.T) {

		pc := GenMockTicker(t, 12*time.Second, 2, 2*time.Second)
		client := &MockRedisHandler{}

		ctx, cancel := context.WithCancel(context.Background())

		var wg sync.WaitGroup

		// Call PollData
		go service.PollData(ctx, "token", "url", pc.Stations, pc.Requester, pc.PollMonitor, client)

		time.Sleep(5 * time.Second)
		fmt.Println("Waited 5 seconds... calling cancel to stop poll")
		cancel()

		//Wait an extra 5 seconds for clean up
		time.Sleep(5 * time.Second)

		wg.Wait()

	})

	t.Run("testing error cancellation on poll data", func(t *testing.T) {

		pc := GenMockTicker(t, 12*time.Second, 2, 2*time.Second)
		client := &MockRedisHandler{}

		ctx, cancel := context.WithCancel(context.Background())

		var wg sync.WaitGroup

		// Call PollData
		go service.PollData(ctx, "token", "url", pc.Stations, pc.Requester, pc.PollMonitor, client)
		go service.MonitorErrors(pc.ErrLog, cancel)

		time.Sleep(5 * time.Second)
		fmt.Println("Waited 5 seconds... sending test error to stop poll")
		err := errors.New("test error")
		pc.ErrLog <- err

		//Wait an extra 5 seconds for clean up
		time.Sleep(5 * time.Second)

		wg.Wait()

	})

	t.Run("testing run poll wrapped function", func(t *testing.T) {

		pc := GenMockTicker(t, 12*time.Second, 2, 2*time.Second)
		client := &MockRedisHandler{}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		go func() {
			service.RunPollService(ctx, cancel, pc, client)
		}()

		time.Sleep(11 * time.Second)

	})

	t.Run("testing window limit reached", func(t *testing.T) {

		pc := GenMockTicker(t, 4*time.Second, 3, 1*time.Second)
		client := &MockRedisHandler{}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		go func() {
			service.RunPollService(ctx, cancel, pc, client)
		}()

		time.Sleep(11 * time.Second)

	})

	t.Run("Basic ticker test", func(t *testing.T) {

		newTicker := time.NewTicker(1 * time.Second)
		pollCount := int(0)

		// Use a loop to repeatedly check the ticker
		for {
			select {
			case t := <-newTicker.C:
				fmt.Printf("Tick at %v\n", t)
				pollCount++
				if pollCount == 3 {
					newTicker.Stop() // Stop the ticker after 3 ticks
					return
				}
			}
		}

	})
}

//==============HELPER FUNCTIONS=================//

func GenMockTicker(t *testing.T, limitWindow time.Duration, reqLimit int, tickerInterval time.Duration) *service.PollConfig {
	t.Helper()

	// GenMockClient is a function made in train_data_test.go and may need to be re-created id test is moved to local module
	clientRequester, _, err := GenMockClient()
	if err != nil {
		t.Fatalf("Error generating mock client: %v", err)
	}

	station := []string{"PAD"}

	ps := &service.PollSetup{
		Token:    "token",
		URL:      "url",
		Stations: station,
	}

	pm := service.NewPollMonitor(reqLimit, limitWindow, tickerInterval)

	return &service.PollConfig{
		PollSetup:   ps,
		PollMonitor: pm,
		Requester:   clientRequester,
	}

}

type MockRedisHandler struct {
	FetchHsetCalled       bool
	FetchJsonCalled       bool
	CheckIDExistCalled    bool
	StoreTrainInfoCalled  bool
	StorePreviousCalled   bool
	StoreSubsequentCalled bool
	PublishCPCalled       bool
	PublishInfoCalled     bool
	PublishFullCalled     bool
}

func (mrh *MockRedisHandler) FetchHSetFromCache(id string) (map[string]string, error) {
	mrh.FetchHsetCalled = true
	id = "test"
	return map[string]string{"test": "test"}, nil
}

func (mrh *MockRedisHandler) FetchJSONFromCache(id string) ([]byte, error) {
	mrh.FetchJsonCalled = true
	id = "test"
	return []byte{}, nil
}

func (mrh *MockRedisHandler) CheckIDExists(id string) (int64, error) {
	mrh.CheckIDExistCalled = true
	return 0, nil
}

func (mrh *MockRedisHandler) StoreTrainInfo(id string, info map[string]string, expiration time.Duration) error {
	mrh.StoreTrainInfoCalled = true
	id = "test"
	info = make(map[string]string)
	info["test"] = "test1"
	expiration = 1 * time.Second
	return nil
}

func (mrh *MockRedisHandler) StorePreviousCallingPoints(id string, mp *models.Service, expiration time.Duration) error {
	mrh.StorePreviousCalled = true
	id = mp.ServiceID
	expiration = 1 * time.Second
	return nil
}

func (mrh *MockRedisHandler) StoreSubsequentCallingPoints(id string, mp *models.Service, expiration time.Duration) error {
	mrh.StoreSubsequentCalled = true
	id = mp.ServiceID
	expiration = 1 * time.Second
	return nil
}

func (mrh *MockRedisHandler) RPublishCP(ctx context.Context, tag string, cp *[]models.CallingPoint) error {
	mrh.PublishCPCalled = true
	ctx.Done()
	tag = "test"
	return nil
}

func (mrh *MockRedisHandler) RPublishInfo(ctx context.Context, tag string, info map[string]string) error {
	mrh.PublishInfoCalled = true
	ctx.Done()
	tag = "test"
	info = make(map[string]string)
	return nil
}

func (mrh *MockRedisHandler) RPublishFull(ctx context.Context, tag string, id string, value *models.ProcessedService) error {
	mrh.PublishFullCalled = true
	ctx.Done()
	tag = "test"
	id = value.Service.ServiceID
	return nil
}

func (mrh *MockRedisHandler) PingRedis() error {
	return nil
}
