package main

import "fmt"

func validateAuthConfig(username, password string) error {
	if username == "" && password == "" {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("username and password must be set together")
	}
	return nil
}

func authEnabled(username, password string) bool {
	return username != "" && password != ""
}
