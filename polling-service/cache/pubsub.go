package cache

import (
	"polling-service/pkg/models"

	"context"
	"encoding/json"
	"fmt"
)

//------------------------------------------------------------
// Model Structs for storing data to be published
//------------------------------------------------------------

type fullService struct {
	StationName         string                `json:"station_name"`
	OriginLocation      string                `json:"origin_location"`
	OriginCRS           string                `json:"origin_crs"`
	DestinationLocation string                `json:"destination_location"`
	DestinationCRS      string                `json:"destination_crs"`
	Previous            []models.CallingPoint `json:"previous"`
	Subsequents         []models.CallingPoint `json:"subsequents"`
}

type infoPayload struct {
	Payload map[string]interface{} `json:"payload"`
}

type cpPayload struct {
	Payload map[string]*[]models.CallingPoint `json:"payload"`
}

type servicePayload struct {
	Payload map[string]*fullService `json:"payload"`
}

//--------------------------------------------------------------
// Publishing data to Redis PubSub
//--------------------------------------------------------------

// TODO Build Pub/Sub Tests, run test publish and test subscribe checks
// TODO Include Train IDs with each publish

type RedisPublisher interface {
	RPublishCP(ctx context.Context, tag string, id string, cp *[]models.CallingPoint) error
	RPublishInfo(ctx context.Context, tag string, id string, info map[string]string) error
	RPublishFull(ctx context.Context, tag string, id string, value *models.ProcessedService) error
}

func (rc *RedisClient) RPublishCP(ctx context.Context, tag string, id string, cp *[]models.CallingPoint) error {

	pl := &cpPayload{Payload: make(map[string]*[]models.CallingPoint, 1)}
	pl.Payload[id] = cp

	// Attempt to publish the message
	jsonValue, err := json.Marshal(pl)
	if err != nil {
		return fmt.Errorf("failed to marshal value to JSON: %w", err)
	}

	// Attempt to publish the message
	result := rc.Client.Publish(ctx, tag, jsonValue)
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

func (rc *RedisClient) RPublishInfo(ctx context.Context, tag string, id string, info map[string]string) error {

	pl := &infoPayload{Payload: make(map[string]interface{}, 1)}
	pl.Payload[id] = info

	jsonValue, err := json.Marshal(pl)
	if err != nil {
		return fmt.Errorf("failed to marshal value to JSON: %w", err)
	}
	result := rc.Client.Publish(ctx, tag, jsonValue)
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

func (rc *RedisClient) RPublishFull(ctx context.Context, tag string, id string, value *models.ProcessedService) error {

	service := &fullService{
		StationName:         value.StationName,
		OriginLocation:      value.Service.Origin.Location,
		OriginCRS:           value.Service.Origin.CRS,
		DestinationLocation: value.Service.Destination.Location,
		DestinationCRS:      value.Service.Destination.CRS,
		Previous:            value.Service.PreviousCallingPoints,
		Subsequents:         value.Service.SubsequentCallingPoints,
	}

	pl := &servicePayload{Payload: make(map[string]*fullService, 1)}
	pl.Payload[id] = service

	jsonValue, err := json.Marshal(pl)
	if err != nil {
		return fmt.Errorf("failed to marshal value to JSON: %w", err)
	}
	result := rc.Client.Publish(ctx, tag, jsonValue)
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}
