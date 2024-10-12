package service

import (
	"polling-service/pkg/models"

	"encoding/xml"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode"
)

// train_data.go Takes the SOAP API response data and unmarshalls the XML into mapped structs
// Once the data is in the mapped structs, a Processing Struct is used to process each service in the station
// The output is a MappedData struct. The struct implements a processing interface to handle
// Processing methods for the service.
// Finally, a concurrent function is defined to go through all 60 major stations and process each service within.
// The Processed Station is added to the MappedData which holds the processed services for each trainID.

type Model struct {
	ProcessedService *models.ProcessedService
	CallingPoint     *models.CallingPoint
}

// ProcessedTrainLine is an interface which the ProcessedService implements. It has methods to process each service
// The interface is used in the ActivateMethods function to handle calling the methods and processing the service data
type ProcessedTrainLine interface {
	GetCurrent(locationName, crs string) (*models.CallingPoint, error)
	CreateServiceID() error
	CreateCombinedLine(current *models.CallingPoint) ([]models.CallingPoint, error)
	OrganiseLine(cl []models.CallingPoint) error
	SetExpiration() error
}

// GetCurrent Method to insert the current station into a current calling point list using the CallingPoint struct
// This is to separate all stops into their respective buckets and ensure that all stops on the line are accounted for
func (ps *Model) GetCurrent(locationName, crs string) (*models.CallingPoint, error) {
	// Set the current stop identifier
	ps.ProcessedService.CurrentStop = crs
	ps.ProcessedService.StationName = locationName

	// Determine the scheduled time and estimated time based on STA and STD
	var st, et string
	if ps.ProcessedService.Service.STA == "" && ps.ProcessedService.Service.STD != "" {
		st = ps.ProcessedService.Service.STD
		et = ps.ProcessedService.Service.ETD
	} else {
		st = ps.ProcessedService.Service.STA
		et = ps.ProcessedService.Service.ETA
	}

	// Create a new CallingPoint for the current location
	cp := &models.CallingPoint{
		LocationName: locationName,
		CRS:          crs,
		ST:           st,
		ET:           et,
		AT:           "",
	}

	return cp, nil
}

// CreateServiceID processing the service service ID - each ID has the station code appended to the end - this method
// removes that to create an ID which is not station specific
func (ps *Model) CreateServiceID() error {
	// Check if the ServiceID is long enough
	if len(ps.ProcessedService.Service.ServiceID) < 7 {
		return fmt.Errorf("ServiceID is too short")
	}

	// Initialize a string builder to construct the new ServiceID
	var newServiceID string

	// Iterate through each character in the ServiceID
	for _, ch := range ps.ProcessedService.Service.ServiceID {
		// Check if the character is a digit
		if unicode.IsDigit(ch) {
			newServiceID += string(ch)
		}
	}

	// If the new ServiceID is empty, return an error
	if newServiceID == "" {
		return fmt.Errorf("ServiceID contains no numeric characters")
	}

	// Update the ServiceID with the new value
	ps.ProcessedService.Service.ServiceID = newServiceID
	return nil
}

// Comparison function for sorting CallingPoints by ST
func compareByST(a, b models.CallingPoint) bool {
	return a.ST < b.ST
}

func (ps *Model) CreateCombinedLine(current *models.CallingPoint) ([]models.CallingPoint, error) {

	previous := ps.ProcessedService.Service.PreviousCallingPoints
	subsequent := ps.ProcessedService.Service.SubsequentCallingPoints

	totalLength := len(previous) + len(subsequent) + 1 // Making extra room to insert current station

	cl := make([]models.CallingPoint, 0, totalLength)

	if len(previous) != 0 {
		cl = append(cl, previous...)
	}
	if len(subsequent) != 0 {
		cl = append(cl, subsequent...)
	}

	cl = append(cl, *current)

	// Sort the CombinedLine slice by the time string (ST)
	sort.Slice(cl, func(i, j int) bool {
		return compareByST(cl[i], cl[j])
	})

	return cl, nil
}

func (ps *Model) SetExpiration() error {

	today := time.Now().Format("2006-01-02")
	now := time.Now()
	var points []models.CallingPoint
	// Determine which set of calling points to use
	if len(ps.ProcessedService.Service.SubsequentCallingPoints) != 0 {
		points = ps.ProcessedService.Service.SubsequentCallingPoints
	} else {
		points = ps.ProcessedService.Service.PreviousCallingPoints
	}

	// Ensure there is at least one calling point
	if len(points) == 0 {
		return fmt.Errorf("no calling points available to set expiration")
	}

	lastStop := points[len(points)-1]
	lastStopTime := lastStop.ST
	targetTimeStr := fmt.Sprintf("%s %s", today, lastStopTime)
	expirationTime, err := time.Parse("2006-01-02 15:04", targetTimeStr)
	if err != nil {
		return err
	}
	// Add 30 minutes buffer to expirationTime
	expirationTime = expirationTime.Add(30 * time.Minute)
	difference := expirationTime.Sub(now)
	if difference < 0 {
		ps.ProcessedService.Expiration = 0
	} else {
		ps.ProcessedService.Expiration = difference
	}
	return nil
}

func ParseTime(timeStr string) (time.Time, error) {
	return time.Parse("15:04", timeStr)
}

func (ps *Model) OrganiseLine(cl []models.CallingPoint) error {

	ps.ProcessedService.Service.PreviousCallingPoints = nil
	ps.ProcessedService.Service.SubsequentCallingPoints = nil

	currentTime := time.Now().Format("15:04")
	currentTimeParsed, err := ParseTime(currentTime)
	if err != nil {
		return err
	}

	previousCap := 0
	subsequentCap := 0

	for _, point := range cl {
		lineTimeParsed, err := ParseTime(point.ST)
		if err != nil {
			return err
		}
		if lineTimeParsed.Before(currentTimeParsed) {
			previousCap++
		} else {
			subsequentCap++
		}
	}

	ps.ProcessedService.Service.PreviousCallingPoints = make([]models.CallingPoint, 0, previousCap)
	ps.ProcessedService.Service.SubsequentCallingPoints = make([]models.CallingPoint, 0, subsequentCap)

	for _, point := range cl {
		lineTimeParsed, err := ParseTime(point.ST)
		if err != nil {
			return err
		}
		if lineTimeParsed.Before(currentTimeParsed) {
			ps.ProcessedService.Service.PreviousCallingPoints = append(ps.ProcessedService.Service.PreviousCallingPoints, point)
		} else {
			ps.ProcessedService.Service.SubsequentCallingPoints = append(ps.ProcessedService.Service.SubsequentCallingPoints, point)
		}
	}

	return nil
}

//TODO may need method to determine if a train route is active or not? Polling from stations which are not busy
// can lead to pulling routes which do not start soon (>1 hour away)

//TODO may need a cancelled method to check if a route has been cancelled to signal to caching function whether to
// remove the route and publish cancel event??

//-------------------------------------------------------------------
// Function to run the process methods and populate the ProcessedService Struct concurrently
//-------------------------------------------------------------------

func ActivateMethods(locationName, currentCRS string, ptl ProcessedTrainLine) error {
	if err := ptl.CreateServiceID(); err != nil {
		return err
	}
	c, err := ptl.GetCurrent(locationName, currentCRS)
	if err != nil {
		return err
	}
	cl, err := ptl.CreateCombinedLine(c)
	if err != nil {
		return err
	}
	if err := ptl.OrganiseLine(cl); err != nil {
		return err
	}
	if err := ptl.SetExpiration(); err != nil {
		return err
	}
	return nil
}

// ProcessTrain function takes an envelope retrieved from a SOAP Request. It parses the XML data, calling a filtering
// Function for only GW Trains. It takes the current station that has been requested from the envelope and proceeds to
// Process each service of the station into a slice of a MappedData struct which is the output of all services from
// That station with the keys being the TrainID and the values being the ProcessedService
func ProcessTrain(data []byte, operatorCode string) (*models.MappedData, error) {

	envelope, err := FilterByOperatorCode(data, operatorCode)
	if err != nil {
		return nil, err
	}

	processedServiceMap := make(map[string]*models.ProcessedService, len(envelope.Body.Response.StationBoardResult.TrainServices))

	// Get current here
	currentLocation := envelope.Body.Response.StationBoardResult.CurrentLocationName
	currentCRS := envelope.Body.Response.StationBoardResult.CRS

	// Process each service in the envelope
	for _, service := range envelope.Body.Response.StationBoardResult.TrainServices {
		ps := &models.ProcessedService{
			Service: &service,
		}

		psModel := &Model{}
		psModel.ProcessedService = ps

		// Additional processing steps
		err = ActivateMethods(currentLocation, currentCRS, psModel)
		if err != nil {
			return nil, err
		}
		processedServiceMap[service.ServiceID] = psModel.ProcessedService

	}

	return &models.MappedData{Map: processedServiceMap}, nil
}

//-------------------------------------------------------------------
// Function to Filter the XML by Operator Code
//-------------------------------------------------------------------

func FilterByOperatorCode(data []byte, operatorCode string) (*models.TrainEnvelope, error) {
	var envelope models.TrainEnvelope

	// Unmarshal XML into the envelope structure
	err := xml.Unmarshal(data, &envelope)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling XML: %v", err)
	}

	// Filter the TrainServices based on the operator code
	var filteredServices []models.Service
	for _, service := range envelope.Body.Response.StationBoardResult.TrainServices {
		if service.OperatorCode == operatorCode {
			filteredServices = append(filteredServices, service)
		}
	}

	// Update the existing StationBoardResult with the filtered services
	envelope.Body.Response.StationBoardResult.TrainServices = filteredServices

	return &envelope, nil
}

//-------------------------------------------------------------------
// Channel Approach - Using channels and a struct to send data to for each processed station
//-------------------------------------------------------------------

type StationsChannel struct {
	Station  string
	Services *models.MappedData
	Err      error
}

// Requester abstracts the Post request made to fetch the API Data. It does so by using the method Post which is
// Implemented by ClientRequester
// The ClientRequester is a struct which wraps the HTTPClient interface handling the requests
type Requester interface {
	Post(url string, payload []byte) ([]byte, error)
}

// TODO Add context to FetchAllTrains for ctx.Done() channel signal

// FetchAllTrains concurrently loops through all stations and fetches the data for each. It then processes the data
// And returns a AllServices map holding []*ProcessedService data as value with each Station as key
func FetchAllTrains(token, url string, stations []string, requester Requester) (*models.MappedData, error) {
	allData := &models.MappedData{Map: make(map[string]*models.ProcessedService)}

	var wg sync.WaitGroup
	var statChan = make(chan StationsChannel, len(stations))
	//var SeenIDs = make(chan []string)
	var mu sync.Mutex

	//var sem = make(chan struct{}, 10) //Can batch if needed

	for _, station := range stations {
		wg.Add(1)
		go func(station string) {
			defer wg.Done()

			// For batching
			//sem <- struct{}{}
			//defer func() { <-sem }()

			request := &GetArrDepBoardWithDetailsRequest{
				NumRows:    10,
				Crs:        "",
				FilterCrs:  "",
				FilterType: "to",
				TimeOffset: 0,
				TimeWindow: 120,
			}
			// Set CRS
			err := request.SetCRS(station)
			if err != nil {
				return
			}
			// Create envelope with request
			envelope, err := NewEnvelope(token, request)
			if err != nil {
				return
			}
			// Change CRS in envelope
			_, err = envelope.ChangeCRS(station)
			if err != nil {
				return
			}

			payload, err := envelope.ToPayload()
			if err != nil {
				statChan <- StationsChannel{Err: err, Station: station}
				return
			}

			data, err := requester.Post(url, payload)
			if err != nil {
				statChan <- StationsChannel{Err: err, Station: station}
				return
			}

			output, err := ProcessTrain(data, "GW")
			if err != nil {
				statChan <- StationsChannel{Err: err, Station: station}
				return
			}

			statChan <- StationsChannel{Services: output, Station: station}
		}(station)
	}

	go func() {
		wg.Wait()
		close(statChan)
	}()

	for result := range statChan {
		if result.Err != nil {
			return nil, result.Err
		}
		mu.Lock()
		for id, service := range result.Services.Map {
			if _, exists := allData.Map[id]; !exists {
				allData.Map[id] = service
			}
		}

		mu.Unlock()
	}

	return allData, nil
}
