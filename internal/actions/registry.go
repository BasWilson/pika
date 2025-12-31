package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/baswilson/pika/internal/ai"
	"github.com/baswilson/pika/internal/calendar"
	"github.com/baswilson/pika/internal/memory"
)

// ActionType represents the type of action
type ActionType string

const (
	ActionSaveToCalendar   ActionType = "SAVE_TO_CALENDAR"
	ActionEditCalendar     ActionType = "EDIT_CALENDAR_EVENT"
	ActionDeleteCalendar   ActionType = "DELETE_CALENDAR_EVENT"
	ActionSaveMemory       ActionType = "SAVE_MEMORY"
	ActionGetWeather       ActionType = "GET_WEATHER"
	ActionSearchPokemon    ActionType = "SEARCH_POKEMON"
	ActionStopListening    ActionType = "STOP_LISTENING"
	ActionNoAction         ActionType = "NO_ACTION"
)

// ActionResult represents the result of executing an action
type ActionResult struct {
	ActionType string      `json:"action_type"`
	Success    bool        `json:"success"`
	Data       interface{} `json:"data,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// ActionHandler is a function that handles a specific action
type ActionHandler func(ctx context.Context, data map[string]interface{}) *ActionResult

// Registry manages action handlers
type Registry struct {
	handlers map[ActionType]ActionHandler
	memory   *memory.Store
	calendar *calendar.Service
}

// NewRegistry creates a new action registry
func NewRegistry(memoryStore *memory.Store, calendarService *calendar.Service) *Registry {
	r := &Registry{
		handlers: make(map[ActionType]ActionHandler),
		memory:   memoryStore,
		calendar: calendarService,
	}

	// Register built-in handlers
	r.Register(ActionSaveToCalendar, r.handleSaveToCalendar)
	r.Register(ActionEditCalendar, r.handleEditCalendar)
	r.Register(ActionDeleteCalendar, r.handleDeleteCalendar)
	r.Register(ActionSaveMemory, r.handleSaveMemory)
	r.Register(ActionGetWeather, r.handleGetWeather)
	r.Register(ActionSearchPokemon, r.handleSearchPokemon)
	r.Register(ActionStopListening, r.handleStopListening)

	return r
}

// Register adds an action handler
func (r *Registry) Register(actionType ActionType, handler ActionHandler) {
	r.handlers[actionType] = handler
}

// Execute runs an action and returns the result
func (r *Registry) Execute(action ai.Action) *ActionResult {
	ctx := context.Background()
	actionType := ActionType(action.Type)

	handler, ok := r.handlers[actionType]
	if !ok {
		return &ActionResult{
			ActionType: action.Type,
			Success:    false,
			Error:      fmt.Sprintf("unknown action type: %s", action.Type),
		}
	}

	log.Printf("Executing action: %s", action.Type)
	result := handler(ctx, action.Data)
	result.ActionType = action.Type

	return result
}

// handleSaveToCalendar creates a calendar event
func (r *Registry) handleSaveToCalendar(ctx context.Context, data map[string]interface{}) *ActionResult {
	title, _ := data["title"].(string)
	description, _ := data["description"].(string)
	startTime, _ := data["start_time"].(string)
	endTime, _ := data["end_time"].(string)
	location, _ := data["location"].(string)

	if title == "" || startTime == "" {
		return &ActionResult{
			Success: false,
			Error:   "title and start_time are required",
		}
	}

	// If end time not provided, default to 1 hour after start
	if endTime == "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			endTime = t.Add(time.Hour).Format(time.RFC3339)
		}
	}

	event, err := r.calendar.CreateEvent(ctx, title, description, startTime, endTime, location)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return &ActionResult{
		Success: true,
		Data:    event,
	}
}

// handleSaveMemory saves a memory
func (r *Registry) handleSaveMemory(ctx context.Context, data map[string]interface{}) *ActionResult {
	content, _ := data["content"].(string)
	importance, _ := data["importance"].(float64)
	tagsInterface, _ := data["tags"].([]interface{})

	if content == "" {
		return &ActionResult{
			Success: false,
			Error:   "content is required",
		}
	}

	// Default importance
	if importance == 0 {
		importance = 0.5
	}

	// Convert tags
	var tags []string
	for _, t := range tagsInterface {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}

	mem, err := r.memory.Create(ctx, content, importance, tags)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return &ActionResult{
		Success: true,
		Data:    mem,
	}
}

// handleEditCalendar updates an existing calendar event
func (r *Registry) handleEditCalendar(ctx context.Context, data map[string]interface{}) *ActionResult {
	eventID, _ := data["event_id"].(string)
	searchTitle, _ := data["search_title"].(string)

	// If no event_id provided, try to find by title
	if eventID == "" && searchTitle != "" {
		events, err := r.calendar.FindEventByTitle(ctx, searchTitle)
		if err != nil || len(events) == 0 {
			return &ActionResult{
				Success: false,
				Error:   fmt.Sprintf("could not find event matching '%s'", searchTitle),
			}
		}
		eventID = events[0].ID
	}

	if eventID == "" {
		return &ActionResult{
			Success: false,
			Error:   "event_id or search_title is required",
		}
	}

	// Get optional update fields
	var title, description, startTime, endTime, location *string
	if v, ok := data["title"].(string); ok && v != "" {
		title = &v
	}
	if v, ok := data["description"].(string); ok {
		description = &v
	}
	if v, ok := data["start_time"].(string); ok && v != "" {
		startTime = &v
	}
	if v, ok := data["end_time"].(string); ok && v != "" {
		endTime = &v
	}
	if v, ok := data["location"].(string); ok {
		location = &v
	}

	event, err := r.calendar.UpdateEvent(ctx, eventID, title, description, startTime, endTime, location)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return &ActionResult{
		Success: true,
		Data:    event,
	}
}

// handleDeleteCalendar deletes a calendar event
func (r *Registry) handleDeleteCalendar(ctx context.Context, data map[string]interface{}) *ActionResult {
	eventID, _ := data["event_id"].(string)
	searchTitle, _ := data["search_title"].(string)

	// If no event_id provided, try to find by title
	if eventID == "" && searchTitle != "" {
		events, err := r.calendar.FindEventByTitle(ctx, searchTitle)
		if err != nil || len(events) == 0 {
			return &ActionResult{
				Success: false,
				Error:   fmt.Sprintf("could not find event matching '%s'", searchTitle),
			}
		}
		eventID = events[0].ID
	}

	if eventID == "" {
		return &ActionResult{
			Success: false,
			Error:   "event_id or search_title is required",
		}
	}

	if err := r.calendar.DeleteEvent(ctx, eventID); err != nil {
		return &ActionResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return &ActionResult{
		Success: true,
		Data:    map[string]string{"deleted": eventID},
	}
}

// WeatherResponse represents weather data from Open-Meteo API
type WeatherResponse struct {
	Location    string  `json:"location"`
	Temperature float64 `json:"temperature"`
	FeelsLike   float64 `json:"feels_like"`
	Humidity    int     `json:"humidity"`
	WindSpeed   float64 `json:"wind_speed"`
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
}

// handleGetWeather fetches weather data from Open-Meteo API
func (r *Registry) handleGetWeather(ctx context.Context, data map[string]interface{}) *ActionResult {
	location, _ := data["location"].(string)
	if location == "" {
		location = "New York" // default location
	}

	// First, geocode the location using Open-Meteo's geocoding API
	geocodeURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=en&format=json",
		url.QueryEscape(location))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(geocodeURL)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("failed to geocode location: %v", err),
		}
	}
	defer resp.Body.Close()

	var geoResult struct {
		Results []struct {
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Country   string  `json:"country"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geoResult); err != nil {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse geocoding response: %v", err),
		}
	}

	if len(geoResult.Results) == 0 {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("location '%s' not found", location),
		}
	}

	geo := geoResult.Results[0]

	// Now fetch weather data
	weatherURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current=temperature_2m,relative_humidity_2m,apparent_temperature,weather_code,wind_speed_10m&temperature_unit=celsius&wind_speed_unit=kmh",
		geo.Latitude, geo.Longitude)

	resp, err = client.Get(weatherURL)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("failed to fetch weather: %v", err),
		}
	}
	defer resp.Body.Close()

	var weatherResult struct {
		Current struct {
			Temperature  float64 `json:"temperature_2m"`
			Humidity     int     `json:"relative_humidity_2m"`
			FeelsLike    float64 `json:"apparent_temperature"`
			WeatherCode  int     `json:"weather_code"`
			WindSpeed    float64 `json:"wind_speed_10m"`
		} `json:"current"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&weatherResult); err != nil {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse weather response: %v", err),
		}
	}

	// Convert weather code to description
	description := weatherCodeToDescription(weatherResult.Current.WeatherCode)

	weather := &WeatherResponse{
		Location:    fmt.Sprintf("%s, %s", geo.Name, geo.Country),
		Temperature: weatherResult.Current.Temperature,
		FeelsLike:   weatherResult.Current.FeelsLike,
		Humidity:    weatherResult.Current.Humidity,
		WindSpeed:   weatherResult.Current.WindSpeed,
		Description: description,
		Unit:        "celsius",
	}

	return &ActionResult{
		Success: true,
		Data:    weather,
	}
}

// weatherCodeToDescription converts WMO weather codes to human-readable descriptions
func weatherCodeToDescription(code int) string {
	codes := map[int]string{
		0:  "Clear sky",
		1:  "Mainly clear",
		2:  "Partly cloudy",
		3:  "Overcast",
		45: "Foggy",
		48: "Depositing rime fog",
		51: "Light drizzle",
		53: "Moderate drizzle",
		55: "Dense drizzle",
		61: "Slight rain",
		63: "Moderate rain",
		65: "Heavy rain",
		71: "Slight snow",
		73: "Moderate snow",
		75: "Heavy snow",
		77: "Snow grains",
		80: "Slight rain showers",
		81: "Moderate rain showers",
		82: "Violent rain showers",
		85: "Slight snow showers",
		86: "Heavy snow showers",
		95: "Thunderstorm",
		96: "Thunderstorm with slight hail",
		99: "Thunderstorm with heavy hail",
	}
	if desc, ok := codes[code]; ok {
		return desc
	}
	return "Unknown"
}

// PokemonResponse represents Pokemon data from PokeAPI
type PokemonResponse struct {
	Name    string   `json:"name"`
	ID      int      `json:"id"`
	Types   []string `json:"types"`
	Height  float64  `json:"height_m"`
	Weight  float64  `json:"weight_kg"`
	Sprite  string   `json:"sprite"`
	Abilities []string `json:"abilities"`
	Stats   map[string]int `json:"stats"`
}

// handleSearchPokemon searches for Pokemon using PokeAPI
func (r *Registry) handleSearchPokemon(ctx context.Context, data map[string]interface{}) *ActionResult {
	query, _ := data["name"].(string)
	if query == "" {
		query, _ = data["query"].(string)
	}
	if query == "" {
		return &ActionResult{
			Success: false,
			Error:   "name or query is required",
		}
	}

	// PokeAPI uses lowercase names
	query = strings.ToLower(strings.TrimSpace(query))

	client := &http.Client{Timeout: 10 * time.Second}
	pokemonURL := fmt.Sprintf("https://pokeapi.co/api/v2/pokemon/%s", url.PathEscape(query))

	resp, err := client.Get(pokemonURL)
	if err != nil {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("failed to search Pokemon: %v", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("Pokemon '%s' not found", query),
		}
	}

	var pokeResult struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Height  int    `json:"height"` // in decimeters
		Weight  int    `json:"weight"` // in hectograms
		Types   []struct {
			Type struct {
				Name string `json:"name"`
			} `json:"type"`
		} `json:"types"`
		Abilities []struct {
			Ability struct {
				Name string `json:"name"`
			} `json:"ability"`
		} `json:"abilities"`
		Sprites struct {
			FrontDefault string `json:"front_default"`
		} `json:"sprites"`
		Stats []struct {
			BaseStat int `json:"base_stat"`
			Stat     struct {
				Name string `json:"name"`
			} `json:"stat"`
		} `json:"stats"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&pokeResult); err != nil {
		return &ActionResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse Pokemon response: %v", err),
		}
	}

	// Extract types
	types := make([]string, len(pokeResult.Types))
	for i, t := range pokeResult.Types {
		types[i] = t.Type.Name
	}

	// Extract abilities
	abilities := make([]string, len(pokeResult.Abilities))
	for i, a := range pokeResult.Abilities {
		abilities[i] = a.Ability.Name
	}

	// Extract stats
	stats := make(map[string]int)
	for _, s := range pokeResult.Stats {
		stats[s.Stat.Name] = s.BaseStat
	}

	pokemon := &PokemonResponse{
		Name:      strings.Title(pokeResult.Name),
		ID:        pokeResult.ID,
		Types:     types,
		Height:    float64(pokeResult.Height) / 10.0, // convert to meters
		Weight:    float64(pokeResult.Weight) / 10.0, // convert to kg
		Sprite:    pokeResult.Sprites.FrontDefault,
		Abilities: abilities,
		Stats:     stats,
	}

	return &ActionResult{
		Success: true,
		Data:    pokemon,
	}
}

// handleStopListening signals the frontend to stop active listening mode
func (r *Registry) handleStopListening(ctx context.Context, data map[string]interface{}) *ActionResult {
	return &ActionResult{
		Success: true,
		Data:    map[string]bool{"stop_listening": true},
	}
}
