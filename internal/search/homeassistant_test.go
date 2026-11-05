package search

import (
	"context"
	"errors"
	"testing"

	"github.com/NiekKeijzer/homenclature/internal/config"
)

func TestHomeAssistant(t *testing.T) {
	f := &fakeApp{authHeader: "Authorization", authValue: "Bearer tok", responses: map[string]string{
		"GET /api/states": `[
			{"entity_id": "sensor.garden_temperature", "state": "11.4", "attributes": {"friendly_name": "Garden temperature", "unit_of_measurement": "°C"}},
			{"entity_id": "light.garden_lights", "state": "on", "attributes": {"friendly_name": "Garden lights"}},
			{"entity_id": "binary_sensor.garden_gate", "state": "off", "attributes": {}},
			{"entity_id": "light.kitchen", "state": "off", "attributes": {"friendly_name": "Kitchen"}},
			{"entity_id": "automation.water_garden", "state": "on", "attributes": {"friendly_name": "Water the plants"}}
		]`,
	}}
	p := setup(t, config.Search{Type: "home-assistant", Limit: 2, Domains: []string{"light", "sensor", "binary_sensor"}}, f)
	// Sorted by name, matched on name or entity ID, automation filtered out by domains.
	checkResults(t, search(t, p, "Garden"), Results{
		Items: []Result{
			{Title: "Garden lights", Detail: "Light", Value: "On", URL: "https://app.example/history?entity_id=light.garden_lights"},
			{Title: "Garden temperature", Detail: "Sensor", Value: "11.4 °C", URL: "https://app.example/history?entity_id=sensor.garden_temperature"},
		},
		Total: 3,
	})
	checkResults(t, search(t, p, "gate"), Results{
		Items: []Result{{Title: "binary_sensor.garden_gate", Detail: "Binary sensor", Value: "Off", URL: "https://app.example/history?entity_id=binary_sensor.garden_gate"}},
		Total: 1,
	})
	if _, err := p.Thumbnail(context.Background(), "x"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("thumbnail: got %v, want ErrInvalidID", err)
	}
}
