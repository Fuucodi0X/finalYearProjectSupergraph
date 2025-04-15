package functions

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync" // Import sync package

	_ "github.com/lib/pq" // PostgreSQL driver

	"hasura-ndc.dev/ndc-go/types"
)

// Global database connection pool and initialization control
var (
	db     *sql.DB
	dbOnce sync.Once
	dbErr  error // Store initialization error
)

// Initialize the database connection pool once
func initDB() {
	dbOnce.Do(func() {
		connURI := os.Getenv("CONNECTION_URI")
		if connURI == "" {
			// Use fmt.Errorf instead of panic for better error handling in init phase
			dbErr = fmt.Errorf("APP_PG_DB_CONNECTION_URI environment variable is not set")
			return
		}

		parsedURI, err := url.Parse(connURI)
		if err != nil {
			dbErr = fmt.Errorf("failed to parse database URI: %w", err)
			return
		}

		// Ensure scheme is postgres
		parsedURI.Scheme = "postgres"

		// Set default sslmode if not present (adjust as needed for your cloud DB)
		// Common values for cloud DBs: "require", "verify-full", "verify-ca"
		// "disable" is often insecure and may not work for cloud providers.
		// Check your database provider's requirements.
		query := parsedURI.Query()
		if !query.Has("sslmode") {
			// Defaulting to require might be safer for cloud deployments
			// Change this based on your DB's requirements
			query.Set("sslmode", "verify-ca") // Example: changed from disable
		}
		parsedURI.RawQuery = query.Encode()

		// Open the connection pool
		db, err = sql.Open("postgres", parsedURI.String())
		if err != nil {
			dbErr = fmt.Errorf("failed to open database connection pool: %w", err)
			return
		}

		// Optional: Configure pool settings (adjust as needed)
		// db.SetMaxOpenConns(10)
		// db.SetMaxIdleConns(5)
		// db.SetConnMaxLifetime(time.Hour)

		// Initial ping to verify connectivity during init (optional but good)
		if err = db.Ping(); err != nil {
			// Close the pool if initial ping fails
			db.Close() // Close the pool if ping fails during setup
			dbErr = fmt.Errorf("failed to ping database during initial setup: %w", err)
			return
		}

		// Consider logging successful initialization
		fmt.Println("Database connection pool initialized successfully")
	})
}

type CheckClearanceArguments struct {
	UserId string `json:"userId"`
}

type CheckClearanceResult struct {
	DisciplineClearance bool `json:"disciplineClearance"`
	LibraryClearance    bool `json:"libraryClearance"`
	DormitaryClearance  bool `json:"dormitaryClearance"`
}

// FunctionCheckClearance is the handler called by Hasura
func FunctionCheckClearance(ctx context.Context, state *types.State, arguments *CheckClearanceArguments) (*CheckClearanceResult, error) {
	// Ensure DB is initialized
	initDB()
	if dbErr != nil {
		// Return the stored initialization error
		// Maybe log this error server-side as well
		fmt.Printf("Database initialization failed: %v\n", dbErr)
		return nil, fmt.Errorf("database connection unavailable: %w", dbErr)
	}
	if db == nil {
		// Should not happen if dbErr is nil, but safety check
		return nil, fmt.Errorf("database connection pool is nil after initialization")
	}

	// Optional: Ping before use to ensure connection is alive, especially for long-lived Lambdas
	// Be mindful this adds latency to each request. Alternatively, rely on the driver's
	// built-in pooling and reconnection logic. If you ping, use context.
	if err := db.PingContext(ctx); err != nil {
		// Log the ping error
		fmt.Printf("Database ping failed before query: %v\n", err)
		// Consider attempting to reconnect or simply return an error
		// Depending on the driver, the pool might handle this automatically on the next QueryRow
		return nil, fmt.Errorf("database connection lost: %w", err)
	}

	// --- Execute Queries ---
	// Note: Pass the global 'db' to your helper functions or execute directly

	studentHasDisplineIssue, err := checkStudentHasDisciplineIssue(ctx, db, arguments.UserId)
	if err != nil {
		fmt.Printf("Error checking discipline issue for %s: %v\n", arguments.UserId, err)
		// Decide how to handle partial failures. Return error or default struct?
		// Returning error is often better for debugging.
		return nil, fmt.Errorf("failed checking discipline: %w", err)
	}

	studentHasBorrowedBook, err := checkStudentHasBorrowedBook(ctx, db, arguments.UserId)
	if err != nil {
		fmt.Printf("Error checking borrowed books for %s: %v\n", arguments.UserId, err)
		return nil, fmt.Errorf("failed checking library: %w", err)
	}

	studentClearedDormitary, err := checkStudentClearedDormitary(ctx, db, arguments.UserId)
	if err != nil {
		fmt.Printf("Error checking dormitary clearance for %s: %v\n", arguments.UserId, err)
		return nil, fmt.Errorf("failed checking dormitary: %w", err)
	}

	// --- Return Result ---
	return &CheckClearanceResult{
		DisciplineClearance: !studentHasDisplineIssue,
		LibraryClearance:    !studentHasBorrowedBook,
		DormitaryClearance:  studentClearedDormitary,
	}, nil
}

// --- Helper Functions (Modified to accept db and context) ---

func checkStudentHasDisciplineIssue(ctx context.Context, db *sql.DB, studentID string) (bool, error) {
	var exists bool
	query := `
        SELECT 
            EXISTS(SELECT 1 FROM "Complaines" WHERE accused_id = $1) OR
            EXISTS(SELECT 1 FROM "Suspensions" WHERE suspended_user_id = $1)
    `
	// Use QueryRowContext for cancellation propagation
	err := db.QueryRowContext(ctx, query, studentID).Scan(&exists)
	if err != nil {
		// Check for sql.ErrNoRows specifically if needed, though EXISTS should always return a row
		return false, fmt.Errorf("error executing discipline existence check: %w", err)
	}
	return exists, nil
}

func checkStudentHasBorrowedBook(ctx context.Context, db *sql.DB, studentID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM "BorrowedBooks" WHERE user_id = $1)`
	err := db.QueryRowContext(ctx, query, studentID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("error executing borrowed book existence check: %w", err)
	}
	return exists, nil
}

func checkStudentClearedDormitary(ctx context.Context, db *sql.DB, studentID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM "DormitaryPlacement" WHERE user_id = $1)`
	err := db.QueryRowContext(ctx, query, studentID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("error executing dormitary placement existence check: %w", err)
	}
	// Logic: Cleared if *no* placement exists
	return !exists, nil
}
