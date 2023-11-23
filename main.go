package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
	_ "github.com/lib/pq"
)

var db *gorm.DB
var err error

const (
	connStr        = "postgresql://postgres:postgres@localhost:5432/formdata?sslmode=disable"
	csvUploadRoute = "/upload-csv"
)

type User struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type Issue struct {
	gorm.Model
	Title      string    `json:"title"`
	Details    string    `json:"details"`
	Priority   int       `json:"priority"`
	Status     bool      `json:"status"`
	Type       bool      `json:"type"`
	ImageURL   string    `json:"imageURL"`
	ReportedBy string    `json:"reportedBy"`
	ReportedAt time.Time `json:"reportedAt"`
}

type BugReport struct {
	gorm.Model
	Title      string    `json:"title"`
	Details    string    `json:"details"`
	Priority   int       `json:"priority"`
	Status     bool      `json:"status"`
	Type       bool      `json:"type"`
	ImageURL   string    `json:"imageURL"`
	ReportedBy string    `json:"reportedBy"`
	ReportedAt time.Time `json:"reportedAt"`
}

func main() {
	// Connect to PostgreSQL database
	db, err = gorm.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// AutoMigrate creates tables based on User, Issue, and BugReport models
	db.AutoMigrate(&User{}, &Issue{}, &BugReport{})

	// Create an admin user
	createAdmin()

	// Initialize Gorilla Mux router
	r := mux.NewRouter()

	// Define routes
	r.HandleFunc("/register", registerHandler).Methods("POST")
	r.HandleFunc("/login", loginHandler).Methods("POST")
	r.HandleFunc(csvUploadRoute, uploadCSVHandler).Methods("POST")
	r.HandleFunc("/login-by-email", loginByEmailHandler).Methods("POST")
	r.HandleFunc("/report-issue", reportIssueHandler).Methods("POST") // Changed the endpoint to /report-issue
	r.HandleFunc("/issues/{id:[0-9]+}", getIssueByIDHandler).Methods("GET")

	// Serve static files (for uploaded images)
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Run the server
	port := ":3000"
	fmt.Printf("Server running on port %s\n", port)
	if err := http.ListenAndServe(port, r); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

func createAdmin() {
	// Check if an admin user already exists
	var admin User
	if db.Where("role = ?", "admin").First(&admin).RecordNotFound() {
		// Create admin user if not exists
		admin := User{Username: "admin", Password: "adminpass", Role: "admin"}
		if err := db.Create(&admin).Error; err != nil {
			log.Fatal("Failed to create admin user:", err)
		}
		fmt.Println("Admin user created successfully.")
	}
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var newUser User
	if err := json.NewDecoder(r.Body).Decode(&newUser); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if the username is already taken
	var existingUser User
	if err := db.Where("username = ?", newUser.Username).First(&existingUser).Error; err == nil {
		http.Error(w, "Username already taken", http.StatusConflict)
		return
	}

	// Create the new user
	if err := db.Create(&newUser).Error; err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "User created successfully"})
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var loginDetails User
	if err := json.NewDecoder(r.Body).Decode(&loginDetails); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if the user exists
	var user User
	if err := db.Where("username = ? AND password = ?", loginDetails.Username, loginDetails.Password).First(&user).Error; err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Login successful", "user": user})
}

func uploadCSVHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20) // 10 MB limit
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("csvFile")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		http.Error(w, "Error reading CSV file", http.StatusInternalServerError)
		return
	}

	saveDataToDatabase(records)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("CSV file uploaded and data saved to database"))
}

func saveDataToDatabase(records [][]string) {
	// Assuming the columns are named "Email Address", "Full Name", "Timestamp", "Twitter Profile", "Linkedin Profile"
	emailIndex, nameIndex, timestampIndex, twitterIndex, linkedinIndex := -1, -1, -1, -1, -1

	// Find the indices of the columns
	if len(records) > 0 {
		headers := records[0]
		for i, header := range headers {
			switch header {
			case "Email Address":
				emailIndex = i
			case "Full Name":
				nameIndex = i
			case "Timestamp":
				timestampIndex = i
			case "Twitter Profile":
				twitterIndex = i
			case "LinkedIn Profile":
				linkedinIndex = i
			}
		}
	}

	// If any required column not found, log an error and return
	if emailIndex == -1 || nameIndex == -1 || timestampIndex == -1 || twitterIndex == -1 || linkedinIndex == -1 {
		log.Println("Error: Required columns not found in CSV")
		return
	}

	// Open a transaction for batch insert
	tx := db.Begin()

	for _, record := range records[1:] { // Skip the header row
		if len(record) > emailIndex {
			email := record[emailIndex]
			name := record[nameIndex]
			timestamp := record[timestampIndex]
			twitter := record[twitterIndex]
			linkedin := record[linkedinIndex]

			// Check if the email already exists in the database
			var existingEmail string
			err := tx.Table("emails").Where("email = ?", email).Select("email").Row().Scan(&existingEmail)
			if err == nil {
				log.Printf("Email %s already exists in the database, skipping insertion", email)
				continue
			} else if err != sql.ErrNoRows {
				log.Printf("Error checking existing email %s: %s", email, err)
				tx.Rollback() // Rollback the transaction on error
				return
			}

			// Insert data into the database using the transaction
			result := tx.Exec("INSERT INTO emails (email, full_name, timestamp, twitter_profile, linkedin_profile) VALUES ($1, $2, $3, $4, $5)",
				email, name, timestamp, twitter, linkedin)
			if result.Error != nil {
				log.Printf("Error inserting data for email %s: %s", email, result.Error)
				tx.Rollback() // Rollback the transaction on error
				return
			}
		}
	}
	tx.Commit()
}

func loginByEmailHandler(w http.ResponseWriter, r *http.Request) {
	var loginDetails struct {
		Email string `json:"email"`
	}

	if err := json.NewDecoder(r.Body).Decode(&loginDetails); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if the email exists in the 'emails' table
	var existingEmail string
	err := db.Table("emails").Where("email = ?", loginDetails.Email).Select("email").Row().Scan(&existingEmail)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Login successful", "email": existingEmail})
}

func reportIssueHandler(w http.ResponseWriter, r *http.Request) {
	var newIssue Issue

	// Parse the JSON request body
	err := json.NewDecoder(r.Body).Decode(&newIssue)
	if err != nil {
		log.Println("Error decoding JSON:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Add the new issue to the database
	err = db.Create(&newIssue).Error
	if err != nil {
		log.Println("Error creating BugReport:", err)
		http.Error(w, "Failed to create issue", http.StatusInternalServerError)
		return
	}

	// Log the created BugReport
	log.Printf("BugReport created: %+v", newIssue)

	// Respond with a success message
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Issue reported successfully"})
}

func getIssueByIDHandler(w http.ResponseWriter, r *http.Request) {
	// Get the ID from the URL parameters
	vars := mux.Vars(r)
	id, ok := vars["id"]

	// Check if ID is empty or invalid
	if !ok || id == "" {
		log.Println("Empty or invalid issue ID")
		http.Error(w, "Invalid issue ID", http.StatusBadRequest)
		return
	}

	// Parse the ID into uint
	issueID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		log.Println("Error parsing ID:", err)
		http.Error(w, "Invalid issue ID", http.StatusBadRequest)
		return
	}

	// Query the database for the issue with the specified ID
	var foundIssue Issue
	err = db.First(&foundIssue, uint(issueID)).Error
	if err != nil {
		log.Println("Error retrieving issue:", err)
		http.Error(w, "Error retrieving issue", http.StatusInternalServerError)
		return
	}

	// Respond with the found issue
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(foundIssue)
}
