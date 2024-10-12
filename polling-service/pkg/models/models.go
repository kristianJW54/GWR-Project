package models

import (
	"encoding/xml"
	"time"
)

// TrainEnvelope which holds the XML output of the SOAP API Request
// Body embeds the TrainBody struct which is the response body of the request.
type TrainEnvelope struct {
	XMLName xml.Name  `xml:"Envelope"`
	Body    TrainBody `xml:"Body"`
}

// TrainBody embeds the response request of the API
type TrainBody struct {
	Response GetArrDepBoardWithDetailsResponse `xml:"GetArrDepBoardWithDetailsResponse"`
}

// GetArrDepBoardWithDetailsResponse is the POST request submitted to the SOAP API, the response holds the station
// board results
type GetArrDepBoardWithDetailsResponse struct {
	StationBoardResult StationBoardResult `xml:"GetStationBoardResult"`
}

// StationBoardResult is the result for the station which the data belongs to e.g "PADDINGTON"
// For that station, the TrainServices holds the list of Train Services running from that station including departures
// and arrivals as specified by the request to the API
type StationBoardResult struct {
	CurrentLocationName string    `xml:"locationName"`
	CRS                 string    `xml:"crs"`
	TrainServices       []Service `xml:"trainServices>service"`
}

// Service struct holds the data for each specific service for the specified station
type Service struct {
	STA                     string         `xml:"sta"`
	ETA                     string         `xml:"eta"`
	STD                     string         `xml:"std"`
	ETD                     string         `xml:"etd"`
	Operator                string         `xml:"operator"`
	OperatorCode            string         `xml:"operatorCode"`
	ServiceID               string         `xml:"serviceID"`
	RSID                    string         `xml:"rsid"`
	Origin                  Origin         `xml:"origin>location"`
	Destination             Destination    `xml:"destination>location"`
	PreviousCallingPoints   []CallingPoint `xml:"previousCallingPoints>callingPointList>callingPoint" json:"previous_calling_points"`
	SubsequentCallingPoints []CallingPoint `xml:"subsequentCallingPoints>callingPointList>callingPoint" json:"subsequent_calling_points"`
}

type Origin struct {
	Location string `xml:"locationName"`
	CRS      string `xml:"crs"`
}

type Destination struct {
	Location string `xml:"locationName"`
	CRS      string `xml:"crs"`
}

// CallingPoint holds the data for each stop along the line.
type CallingPoint struct {
	LocationName string `xml:"locationName" json:"location_name"`
	CRS          string `xml:"crs" json:"crs"`
	ST           string `xml:"st" json:"st"`
	AT           string `xml:"at" json:"at"`
	ET           string `xml:"et" json:"et"`
}

//----------------------------------------------------------------
// Processing the Envelope to a Processed Service Struct
//----------------------------------------------------------------

// ProcessedService points to the Service struct and uses the data to process the service service into the desired
// Output.
// The struct is used by the ProcessTrain Function which takes the all the service services for a station and processes
// Them using the ProcessedService struct and returns a slice of the struct to hold each service for that station
// e.g []*ProcessedService
type ProcessedService struct {
	StationName string        `json:"stationName"`
	Service     *Service      `json:"service"`
	CurrentStop string        `json:"currentStop"`
	Expiration  time.Duration `json:"expiration"` //Last stop time + 1 hour as buffer
	IsCancelled bool          `json:"isCancelled"`
}

type MappedData struct {
	Map map[string]*ProcessedService `json:"map"`
}
