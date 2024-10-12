package service

import (
	"bytes"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
)

//TODO Add detailed network logging for request and response times...

const (
	Soapenv = "http://schemas.xmlsoap.org/soap/envelope/"
	Soaptyp = "http://thalesgroup.com/RTTI/2013-11-28/Token/types"
	Soapldb = "http://thalesgroup.com/RTTI/2021-11-01/ldb/"
)

// Envelope represents the SOAP envelope
type Envelope struct {
	XMLName xml.Name `xml:"soapenv:Envelope"`
	Xmlns   string   `xml:"xmlns:soapenv,attr"`
	Typ     string   `xml:"xmlns:typ,attr"`
	Ldb     string   `xml:"xmlns:ldb,attr"`
	Header  Header   `xml:"soapenv:Header"`
	Body    Body     `xml:"soapenv:Body"`
}

type Header struct {
	AccessToken AccessToken `xml:"typ:AccessToken"` //Using AccessToken Struct to access the token struct token
}

type AccessToken struct {
	TokenValue string `xml:"typ:TokenValue"`
}

type Body struct {
	Content interface{} `xml:",any"`
}

type GetArrDepBoardWithDetailsRequest struct {
	XMLName    xml.Name `xml:"ldb:GetArrDepBoardWithDetailsRequest"`
	NumRows    int      `xml:"ldb:numRows"`
	Crs        string   `xml:"ldb:crs"`
	FilterCrs  string   `xml:"ldb:filterCrs,omitempty"` // Optional
	FilterType string   `xml:"ldb:filterType,omitempty"`
	TimeOffset int      `xml:"ldb:timeOffset,omitempty"`
	TimeWindow int      `xml:"ldb:timeWindow,omitempty"`
}

type Payload struct {
	payload []byte
}

//----------------------------------------------
// Functions to construct envelope and payloads
//----------------------------------------------

// NewEnvelope creates a new Envelope with the provided token and request
func NewEnvelope(token string, request interface{}) (*Envelope, error) {
	if len(token) == 0 {
		return nil, fmt.Errorf("token is empty")
	}
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	return &Envelope{
		XMLName: xml.Name{Local: "Envelope", Space: Soapenv},
		Xmlns:   Soapenv,
		Typ:     Soaptyp,
		Ldb:     Soapldb,
		Header: Header{
			AccessToken: AccessToken{
				TokenValue: token,
			},
		},
		Body: Body{
			Content: request,
		},
	}, nil
}

// Interface made to enable changing of CRS code within the Envelope struct after creation
// Will enable dynamic changing if the request body implements a different contents struct
// To be moved to where it is implemented.

type CRSSetter interface {
	SetCRS(crs string) error
	GetCRS() (string, error)
}

// Sets the CRS Code for the body request

func (body *GetArrDepBoardWithDetailsRequest) SetCRS(crs string) error {
	if len(crs) == 0 {
		return fmt.Errorf("crs is empty")
	}
	body.Crs = crs
	return nil
}

func (body *GetArrDepBoardWithDetailsRequest) GetCRS() (string, error) {
	crs := body.Crs
	if crs == "" {
		return "", fmt.Errorf("crs is empty")
	}
	return crs, nil
}

// Changes the CRS code within the Envelope struct using a struct passed as an interface

func (req *Envelope) ChangeCRS(crs string) (*Envelope, error) {
	if crs == "" {
		return nil, errors.New("must provide a CRS code")
	}

	// Assert that the Content is of type CRSSetter Interface - because the request struct implements this interface
	// The struct will be passed as an interface

	if content, ok := req.Body.Content.(CRSSetter); ok {
		// Update the CRS code
		err := content.SetCRS(crs)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("unexpected content type in envelope body")
	}

	return req, nil
}

func (req *Envelope) RetrieveCRS() (string, error) {
	if content, ok := req.Body.Content.(CRSSetter); ok {
		return content.GetCRS()
	}
	return "", errors.New("unexpected content type in envelope body")
}

func (req *Envelope) ToPayload() ([]byte, error) {
	payload, err := xml.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Return the XML as a byte slice
	return payload, nil
}

//----------------------------------------------
// Creating a Client and HTTP Request to fetch data
//----------------------------------------------

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Implementation of the HTTP client interface using the standard library

type StandardHTTPClient struct{}

func (c *StandardHTTPClient) Do(req *http.Request) (*http.Response, error) {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	return client.Do(req)
}

type ClientRequester struct {
	Client HTTPClient
}

//TODO can add context param to Post method with values to track scope and any timeouts/cancellation + logging

func (cr *ClientRequester) Post(url string, payload []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")

	// Send the request using the injected client
	resp, err := cr.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	// Return the response body and nil error
	return body, nil
}

func NewClientRequester() *ClientRequester {
	httpClient := &StandardHTTPClient{}
	return &ClientRequester{
		Client: httpClient,
	}
}
