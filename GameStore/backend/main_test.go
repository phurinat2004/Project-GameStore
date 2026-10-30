package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeGameStore struct {
	games []Game
	query string
	err   error
}

func (store *fakeGameStore) ListGames(_ context.Context, query string) ([]Game, error) {
	store.query = query
	return store.games, store.err
}

type fakeHealth struct {
	err error
}

func (health fakeHealth) PingContext(context.Context) error {
	return health.err
}

func TestHealthReturnsOKWhenDatabaseIsAvailable(t *testing.T) {
	server := api{games: &fakeGameStore{}, health: fakeHealth{}}
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want %q", body["status"], "ok")
	}
}

func TestHealthReturnsUnavailableWhenDatabaseIsDown(t *testing.T) {
	server := api{games: &fakeGameStore{}, health: fakeHealth{err: errors.New("database unavailable")}}
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestListGamesReturnsJSONAndSearchQuery(t *testing.T) {
	store := &fakeGameStore{games: []Game{{
		ID:          1,
		Title:       "Starbound",
		Description: "Explore the stars.",
		Genre:       "Adventure",
		Price:       29.99,
		ReleaseDate: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
	}}}
	server := api{games: store, health: fakeHealth{}}
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/games?q=space", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if store.query != "space" {
		t.Fatalf("query = %q, want %q", store.query, "space")
	}
	var games []Game
	if err := json.Unmarshal(response.Body.Bytes(), &games); err != nil {
		t.Fatalf("decode games response: %v", err)
	}
	if len(games) != 1 || games[0].Title != "Starbound" {
		t.Fatalf("games = %#v, want one Starbound game", games)
	}
}
