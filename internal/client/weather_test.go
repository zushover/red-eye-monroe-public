package client

import (
	"encoding/json"
	"testing"
)

func TestOpenMeteoNullsRemainMissing(t *testing.T) {
	var raw openMeteo
	err := json.Unmarshal([]byte(`{"timezone":"UTC","hourly":{"time":["2026-09-14T00:00"],"temperature_2m":[null],"cloud_cover":[null],"precipitation_probability":[null],"wind_speed_10m":[null]}}`), &raw)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Hourly.Temperature[0] != nil || raw.Hourly.CloudCover[0] != nil || raw.Hourly.PrecipProbability[0] != nil || raw.Hourly.WindSpeed[0] != nil {
		t.Fatal("null weather values must not decode as zero")
	}
}
