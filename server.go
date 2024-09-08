package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
)

var (
	databases = make(map[string]*badger.DB)
	dbLock    sync.RWMutex
	blobDir   = "dbs/blobs" // Directory to store large data blobs
)
var authDB *badger.DB

func databaseHandler(w http.ResponseWriter, r *http.Request) {
	// Extract database name from the URL path
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 {
		http.Error(w, "Invalid request URL", http.StatusBadRequest)
		return
	}
	dbName := parts[0]
	var key string
	if len(parts) > 1 {
		key = parts[1]
	} else {
		key = ""
	}

	// Ensure authentication is performed
	token := r.Header.Get("Authorization")
	if token == "" {
		http.Error(w, "Missing Authorization token", http.StatusUnauthorized)
		return
	}

	// Authenticate token and determine access
	perms, err := authenticate(authDB, token, dbName)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Access the requested database
	var db *badger.DB
	if dbName == "auth" {
		db = authDB
	} else {
		// Open other databases based on your setup
		db, err = getDatabase(dbName)
		if err != nil {
			fmt.Printf("Error while getting database %s: %s", dbName, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	if db == nil {
		http.Error(w, "Database not found", http.StatusNotFound)
		return
	}

	//Handle list operation (get operation on the db itself)
	if key == "" && r.Method == http.MethodGet {
		handleList(db, perms, w, r)
		return
	}

	// Handle GET, POST, DELETE operations on the selected database
	switch r.Method {
	case http.MethodGet:
		handleGet(db, key, perms, w, r)
	case http.MethodPost:
		if !perms.Readonly {
			handlePost(db, key, perms, w, r)
		} else {
			http.Error(w, "Method not allowed (Token is readonly)", http.StatusMethodNotAllowed)
		}
	case http.MethodDelete:
		if !perms.Readonly {
			handleDelete(db, key, perms, w, r)
		} else {
			http.Error(w, "Method not allowed (Token is readonly)", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Open or create a BadgerDB instance
func getDatabase(name string) (*badger.DB, error) {
	dbLock.RLock()
	db, ok := databases[name]
	dbLock.RUnlock()
	if ok {
		return db, nil
	}

	dbLock.Lock()
	defer dbLock.Unlock()

	// Recheck under the lock
	if db, ok := databases[name]; ok {
		return db, nil
	}

	// Open or create BadgerDB
	opts := badger.DefaultOptions("dbs/" + fmt.Sprintf("%s.db", name)).WithLogger(nil)
	newDB, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	databases[name] = newDB
	return newDB, nil
}

func main() {
	fmt.Println("Auth db token: ", authDBToken)
	var err error
	authDB, err = badger.Open(badger.DefaultOptions("dbs/auth.db").WithLogger(nil))
	if err != nil {
		log.Fatal(err)
	}
	defer authDB.Close()

	// Create blob storage directory
	if err := os.MkdirAll(blobDir, os.ModePerm); err != nil {
		log.Fatal(err)
	}

	// Set up the HTTP routes
	rateLimiter := NewRateLimiter(10, time.Minute)

	http.HandleFunc("/", rateLimiter.Limit(authMiddleware(databaseHandler)))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
