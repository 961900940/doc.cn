package main

import "testing"

func TestSetupPasswordMessage(t *testing.T) {
	msg := validateSelfPassword("123")
	if msg == "" {
		t.Fatal("expected weak password error")
	}
	if validateSelfPassword("Admin123!") != "" {
		t.Fatal("expected strong password to pass")
	}
}
