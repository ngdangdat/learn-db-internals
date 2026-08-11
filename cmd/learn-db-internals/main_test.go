package main

import "testing"

func TestWelcomeMessage(t *testing.T) {
	const want = "learn-db-internals: database internals experiments"

	if welcomeMessage != want {
		t.Fatalf("welcomeMessage = %q, want %q", welcomeMessage, want)
	}
}
