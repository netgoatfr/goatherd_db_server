package main

import (
	"encoding/json"
	"errors"

	"github.com/dchest/uniuri"
	"github.com/dgraph-io/badger/v3"
)

// Structure to represent token permissions
type TokenPermissions struct {
	Databases      []string `json:"databases"`
	Ratelimit      int      `json:"ratelimit"` // 0: default; -1: No limit; more than 0: this number
	Readonly       bool     `json:"Readonly"`
	MaxStoringSize int64    `json:"MaxStoringSize"` // Default is 1GB
}

func genToken() string {
	return uniuri.NewLen(30) + "-" + uniuri.NewLen(30) + "-" + uniuri.NewLen(30)
}

var authDBToken = genToken()

// Helper function to retrieve token permissions from the auth database
func getTokenPermissions(authDB *badger.DB, token string) (TokenPermissions, error) {
	var permissions TokenPermissions = TokenPermissions{
		Databases: []string{},
		MaxStoringSize: 1024 * 1024 * 1024
	}

	// Check if the token exists in the authDB
	err := authDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(token))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val[6:], &permissions) // Parse the stored JSON
		})
	})

	if err == badger.ErrKeyNotFound {
		return permissions, errors.New("invalid token")
	} else if err != nil {
		return permissions, err
	}

	_ = authDB.Update(func(txn *badger.Txn) error { // Modify the value in the db to add any missing values (ex: the stored value was an empty dict)
		d, _ := json.Marshal(permissions)
		return txn.Set([]byte(token), append([]byte(normalPrefix), d...))
	})

	return permissions, nil
}

// Middleware function to authenticate based on the token and requested database
func authenticate(authDB *badger.DB, token, dbName string) (TokenPermissions, error) {
	if token == authDBToken {
		// Allow access only to the 'auth' database for the default admin token
		if dbName == "auth" {
			return nil // Token is authorized for the 'auth' database
		}
		return errors.New("token not authorized for this database")
	}

	permissions, err := getTokenPermissions(authDB, token)
	if err != nil {
		return nil,err // Token not found or invalid
	}

	// Check if the token allows access to the requested database
	for _, allowedDB := range permissions.Databases {
		if allowedDB == dbName {
			return permissions,nil // Token is authorized for this database
		}
	}

	return nil,errors.New("token not authorized for this database")
}
