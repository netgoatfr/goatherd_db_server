package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgraph-io/badger/v3"
)

const blobPrefix = "BLOB__"
const normalPrefix = "VALUE_"

// Retrieve a key from the database, stream blob if the value is a file path
func handleGet(db *badger.DB, key string, _ TokenPermissions, w http.ResponseWriter, _ *http.Request) {
	var value []byte

	// Get the value from the database
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			value = append([]byte{}, val...)
			return nil
		})
	})
	if err == badger.ErrKeyNotFound {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to retrieve key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if the value is a file path (i.e., a blob was stored)
	prefix := string(value)[0:6]
	value = value[6:]
	if prefix == blobPrefix {
		// The value is a file path, so open the blob file for streaming
		blobFile := string(value)
		file, err := os.Open(blobFile)
		if err != nil {
			http.Error(w, "Failed to open blob file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Set appropriate headers for streaming large files
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(blobFile)))

		// Stream the file content directly to the response without loading it all into memory
		_, err = io.Copy(w, file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	} else if prefix == normalPrefix {
		w.Write(value)
	}
}

// Set a key-value pair in the database, store large data as blobs
func handlePost(db *badger.DB, key string, perms TokenPermissions, w http.ResponseWriter, r *http.Request) {
	buffer := make([]byte, 4096) // Read in chunks of 1KB
	n, _ := r.Body.Read(buffer)
	buffer = buffer[:n] // Remove unwanted null bytes.
	defer r.Body.Close()
	var value []byte
	// If the value is large, store it as a blob in the filesystem
	var max_possible_size = perms.MaxStoringSize - perms.CurrentlyStoredSize
	if len(buffer) > max_possible_size {
		http.Error(w, "This token can not store any more data.", http.StatusForbidden)
		return
	}
	if len(buffer) > 1024 { // For example, values > 1KB are treated as blobs
		blobFile := filepath.Join(blobDir, key)
		file, err := os.OpenFile(blobFile, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			http.Error(w, "Failed to write blob: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var size int

		// Write the blob to the filesystem
		for {
			size += n
			if size >= max_possible_size {
				http.Error(w, "This token can not store any more data.", http.StatusForbidden)
				file.Close()
				os.Remove(blobFile)
				return
			}
			file.Write(buffer) // Write the buffer to the file

			// get new buffer
			n, err = r.Body.Read(buffer)
			if err != nil {
				if err == io.EOF {
					// End of body
					file.Close()
					break
				}
				http.Error(w, "Failed to write blob: "+err.Error(), http.StatusInternalServerError)
				file.Close()
				return
			}
		}
		// Store the file path in BadgerDB
		value = []byte(blobPrefix + blobFile)
	} else {
		value = append([]byte(normalPrefix), buffer...)
	}

	// Store the value or file path in the database
	err := db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), value)
	})
	if err != nil {
		http.Error(w, "Failed to set key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Delete a key and its corresponding blob (if exists)
func handleDelete(db *badger.DB, key string, _ TokenPermissions, w http.ResponseWriter, _ *http.Request) {
	// First, check if the key exists and if it points to a blob
	var value []byte
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			value = append([]byte{}, val...)
			return nil
		})
	})
	if err == badger.ErrKeyNotFound {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to retrieve key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// If the value is a blob path, delete the blob file
	if strings.HasPrefix(string(value), blobDir) {
		err = os.Remove(string(value))
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "Failed to delete blob", http.StatusInternalServerError)
			return
		}
	}

	// Delete the key from the database
	err = db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
	if err != nil {
		http.Error(w, "Failed to delete key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Delete a key and its corresponding blob (if exists)
func handleList(db *badger.DB, _ TokenPermissions, w http.ResponseWriter, _ *http.Request) {
	// First, check if the key exists and if it points to a blob
	var keys []string
	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()
			keys = append(keys, string(key))
		}
		return nil
	})
	if err != nil {
		http.Error(w, "Failed to retrieve list: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(keys) == 0 {
		w.Write([]byte("[]"))
		return
	}
	data, _ := json.Marshal(keys)
	w.Write(data)
}

// Middleware to authenticate based on the token for the database
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var token string
		token = r.Header.Get("Authorization")
		if token == "" {
			if len(strings.Split(r.RequestURI, "?")) > 1 {
				token = strings.Split(r.RequestURI, "?")[1]
			} else {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Extract db name from the path
		path := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 1 {
			http.Error(w, "Invalid request URL", http.StatusBadRequest)
			return
		}
		dbName := parts[0]

		// Authenticate the token for the current database
		_, err := authenticate(authDB, token, dbName)
		if err != nil {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// Call the next handler if authentication is successful
		next.ServeHTTP(w, r)
	}
}
