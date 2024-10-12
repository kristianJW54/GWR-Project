package tests

import (
	service2 "GWR_Project/cmd/polling-service/service"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkProcessTrainSample(b *testing.B) {
	// Ensure SAMPLE_OUTPUT environment variable is set

	// Get the current working directory
	path, err := os.Getwd()
	if err != nil {
		b.Fatalf("Error getting current directory: %v", err)
	}

	// Move one directory up using ".." and then construct the full path to the XML file
	filePath := filepath.Join(path, "..", "outputs", "sample_output.xml")

	// Open the XML file
	file, err := os.Open(filePath)
	if err != nil {
		b.Fatalf("Error opening file: %v", err)
	}
	defer file.Close()

	// Read the file content
	bytesData, err := io.ReadAll(file)
	if err != nil {
		b.Fatalf("Error reading file: %v", err)
	}

	// Benchmark the ProcessTrain function
	b.ResetTimer() // Reset the timer to only measure the ProcessTrain function execution time
	for i := 0; i < b.N; i++ {
		_, _ = service2.ProcessTrain(bytesData, "GW")
	}
}

func TestPostRequest(t *testing.T) {

	clientRequester, responseBody, err := GenMockClient()
	if err != nil {
		t.Fatalf("Error creating clientRequester: %v", err)
	}

	// Test url and payload (the payload is insignificant here as the response has already been set
	url := "https://lite.realtime.nationalrail.co.uk/OpenLDBWS/ldb12.asmx"
	payload := []byte("<SomeSOAPRequest>Payload</SomeSOAPRequest>")

	// Make the POST request using the mocked client
	resp, err := clientRequester.Post(url, payload)
	if err != nil {
		t.Fatalf("Error making POST request: %v", err)
	}

	// Assert the response is as expected
	if !bytes.Equal(resp, responseBody) {
		t.Errorf("Expected response %s, got %s", string(responseBody), string(resp))
	}
}

//--------------------------------------------------------
// Benchmark Testing fetch all data
//--------------------------------------------------------

// Cycling through 60 stations all of which are PAD to align with the sample output
// The test is to benchmark the concurrency of processing and requesting data using mock HTTPClient and sample output
// data
// --

func BenchmarkFetchAllTrains(b *testing.B) {

	token := "ABC"
	url := "https://lite.realtime.nationalrail.co.uk/OpenLDBWS/ldb12.asmx"

	stations, err := SixtyPADStations()
	if err != nil {
		b.Fatalf("Error fetching stations: %v", err)
	}

	clientRequester, _, err := GenMockClient()
	if err != nil {
		b.Fatalf("Error creating clientRequester: %v", err)
	}

	// Benchmark the ProcessTrain function
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service2.FetchAllTrains(token, url, stations, clientRequester)
	}

}

//--------------------------------------------------------
// Testing handling data output - unmarshalling to json - mapped data
//--------------------------------------------------------

func TestFetchAllTrainsWithMockClient(t *testing.T) {
	// Generate the mock client
	clientRequester, _, err := GenMockClient()
	if err != nil {
		t.Fatalf("Error generating mock client: %v", err)
	}

	// Create test data
	stations, err := SixtyPADStations()
	if err != nil {
		t.Fatalf("Error fetching stations: %v", err)
	}

	// Call the function with the mock client
	mappedData, err := service2.FetchAllTrains("token", "url", stations, clientRequester)
	if err != nil {
		t.Fatalf("Error fetching trains: %v", err)
	}

	// Additional test assertions can be added here
	for k, v := range mappedData.Map {
		t.Logf("Station: %s, Data: %v", k, v)
	}
}

// Loading sample outputs

// LoadSampleOutput reads the contents of a file specified by an environment variable.
// `name` should be the name of the environment variable holding the file path.
func LoadSampleOutput() ([]byte, error) {

	// Open the file
	// Get the current working directory
	path, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("error getting current directory: %v", err)
	}
	// Move one directory up using ".." and then construct the full path to the XML file
	filePath := filepath.Join(path, "..", "outputs", "sample_output.xml")
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file %v: %w", file, err)
	}
	defer file.Close()

	// Read the file contents
	bytesData, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("error reading file %v: %w", file, err)
	}

	return bytesData, nil
}

// Function to create 60 stations of the same station in the sample output for process testing

func SixtyPADStations() ([]string, error) {
	var station []string
	for i := 0; i < 60; i++ {
		station = append(station, "PAD")
	}
	if len(station) != 60 {
		return station, fmt.Errorf("expected 60 stations, got %v", len(station))
	}
	return station, nil
}

// Creating test http response requests with mock interface injected

// MockHttpClient implements the HTTPClient interface. It specifies a MockDo function which is called
// When the Do method is invoked
// --

type MockHttpClient struct {
	MockDo func(req *http.Request) (*http.Response, error)
}

func (m *MockHttpClient) Do(req *http.Request) (*http.Response, error) {
	return m.MockDo(req)
}

// GenMockClient loads a sample output, creates a mock client instance and a MockDo response to be returned by
// the HTTPClient .Do() method
// It then creates a ClientRequester which wraps the HTTPClient and passes the concrete mock client as a value
// of the HTTPClient interface
// It then returns the ClientRequester, the responseBody and any errors
func GenMockClient() (*service2.ClientRequester, []byte, error) {
	// Load the sample output to insert into the response body
	responseBody, err := LoadSampleOutput()
	if err != nil {
		return nil, nil, err
	}
	// mockClient creates a MockHttpClient and invokes the MockDo function which creates a test response
	// with the sample output as the body
	mockClient := &MockHttpClient{
		MockDo: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBuffer(responseBody)),
				Header:     make(http.Header),
			}, nil
		},
	}
	// A ClientRequester instance is created - the struct wraps the HTTPClient interface, so passing the
	// mockClient as it's value will return the MockDo function inside as Do() method is called.
	clientRequester := &service2.ClientRequester{
		Client: mockClient,
	}
	return clientRequester, responseBody, nil
}
