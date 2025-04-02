package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	windowSize     = 10
	timeout        = 500 * time.Millisecond
	testServerBase = "http://20.244.56.144/evaluation-service/"
	manualToken    = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJNYXBDbGFpbXMiOnsiZXhwIjoxNzQzNjA0NzY4LCJpYXQiOjE3NDM2MDQ0NjgsImlzcyI6IkFmZm9yZG1lZCIsImp0aSI6IjMyMDBlNTJhLTI2ZWUtNGY4NS04YmMyLTI2ZDM0ZjQxMTg1MCIsInN1YiI6IjIyMDUyMTQwQGtpaXQuYWMuaW4ifSwiZW1haWwiOiIyMjA1MjE0MEBraWl0LmFjLmluIiwibmFtZSI6InJhbml0IG11a2hlcmplZSIsInJvbGxObyI6IjIyMDUyMTQwIiwiYWNjZXNzQ29kZSI6Im53cHdyWiIsImNsaWVudElEIjoiMzIwMGU1MmEtMjZlZS00Zjg1LThiYzItMjZkMzRmNDExODUwIiwiY2xpZW50U2VjcmV0IjoiUWdHaEtWRFZ6QUNXY2tkWCJ9.skrbsC46SzNw9M9NQ5Z9rkq-NiTZSlAQBlIlRpzTTHk"
)

type NumberStore struct {
	mu      sync.Mutex
	numbers map[string][]int
}

type Response struct {
	WindowPrevState []int   `json:"windowPrevState"`
	WindowCurrState []int   `json:"windowCurrState"`
	Numbers         []int   `json:"numbers"`
	Avg             float64 `json:"avg"`
}

var (
	store = NumberStore{numbers: make(map[string][]int)}
)

var endpointMap = map[string]string{
	"p": "primes",
	"f": "fibo",
	"e": "even",
	"r": "rand",
}

func main() {
	http.HandleFunc("/numbers/", handleNumbers)
	log.Println("Server started on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleNumbers(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	id := r.URL.Path[len("/numbers/"):]
	if !isValidID(id) {
		http.Error(w, "Invalid number ID", http.StatusBadRequest)
		return
	}

	numbers, err := fetchNumbers(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := processNumbers(id, numbers)

	if time.Since(startTime) > timeout {
		http.Error(w, "Request timeout", http.StatusRequestTimeout)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func isValidID(id string) bool {
	_, exists := endpointMap[id]
	return exists
}

func fetchNumbers(id string) ([]int, error) {
	client := http.Client{Timeout: timeout}
	endpoint := endpointMap[id]
	url := testServerBase + endpoint

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Add("Authorization", "Bearer "+manualToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch numbers: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("test server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	var data struct {
		Numbers []int `json:"numbers"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse numbers: %v", err)
	}

	return data.Numbers, nil
}

func processNumbers(id string, newNumbers []int) Response {
	store.mu.Lock()
	defer store.mu.Unlock()

	prevState := make([]int, len(store.numbers[id]))
	copy(prevState, store.numbers[id])

	uniqueNewNumbers := make([]int, 0)
	for _, num := range newNumbers {
		if !contains(store.numbers[id], num) {
			uniqueNewNumbers = append(uniqueNewNumbers, num)
		}
	}

	for _, num := range uniqueNewNumbers {
		if len(store.numbers[id]) >= windowSize {
			store.numbers[id] = store.numbers[id][1:]
		}
		store.numbers[id] = append(store.numbers[id], num)
	}

	var sum int
	for _, num := range store.numbers[id] {
		sum += num
	}
	avg := 0.0
	if len(store.numbers[id]) > 0 {
		avg = float64(sum) / float64(len(store.numbers[id]))
	}

	return Response{
		WindowPrevState: prevState,
		WindowCurrState: store.numbers[id],
		Numbers:         newNumbers,
		Avg:             round(avg, 2),
	}
}

func contains(slice []int, num int) bool {
	for _, n := range slice {
		if n == num {
			return true
		}
	}
	return false
}

func round(value float64, places int) float64 {
	pow := 1.0
	for i := 0; i < places; i++ {
		pow *= 10
	}
	return float64(int(value*pow+0.5)) / pow
}
