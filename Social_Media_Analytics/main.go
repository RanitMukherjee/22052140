package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	windowSize     = 10
	timeout        = 10 * time.Second
	testServerBase = "http://20.244.56.144/evaluation-service/"
	manualToken    = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJNYXBDbGFpbXMiOnsiZXhwIjoxNzQzNjA4MTU4LCJpYXQiOjE3NDM2MDc4NTgsImlzcyI6IkFmZm9yZG1lZCIsImp0aSI6IjMyMDBlNTJhLTI2ZWUtNGY4NS04YmMyLTI2ZDM0ZjQxMTg1MCIsInN1YiI6IjIyMDUyMTQwQGtpaXQuYWMuaW4ifSwiZW1haWwiOiIyMjA1MjE0MEBraWl0LmFjLmluIiwibmFtZSI6InJhbml0IG11a2hlcmplZSIsInJvbGxObyI6IjIyMDUyMTQwIiwiYWNjZXNzQ29kZSI6Im53cHdyWiIsImNsaWVudElEIjoiMzIwMGU1MmEtMjZlZS00Zjg1LThiYzItMjZkMzRmNDExODUwIiwiY2xpZW50U2VjcmV0IjoiUWdHaEtWRFZ6QUNXY2tkWCJ9.ELx4XQnE5uXOX2b3JOqEm5YIR4yMK-JYoy-Oi2F9SlM"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Post struct {
	ID      int    `json:"id"`
	UserID  int    `json:"userId"`
	Content string `json:"content"`
}

type Comment struct {
	ID      int    `json:"id"`
	PostID  int    `json:"postId"`
	Content string `json:"content"`
}

type UserWithPostCount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PostCount int    `json:"postCount"`
}

type PostWithCommentCount struct {
	ID           string `json:"id"`
	UserID       string `json:"userId"`
	UserName     string `json:"userName"`
	Content      string `json:"content"`
	CommentCount int    `json:"commentCount"`
}

type Client struct {
	client       *http.Client
	rateLimiter  chan time.Time
	requestCount int
	mu           sync.Mutex
}

func NewClient() *Client {
	limiter := make(chan time.Time, windowSize)
	for i := 0; i < windowSize; i++ {
		limiter <- time.Now()
	}

	go func() {
		for t := range time.Tick(timeout / windowSize) {
			select {
			case limiter <- t:
			default:
			}
		}
	}()

	return &Client{
		client:      &http.Client{Timeout: timeout},
		rateLimiter: limiter,
	}
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	<-c.rateLimiter

	c.mu.Lock()
	c.requestCount++
	count := c.requestCount
	c.mu.Unlock()

	fmt.Printf("Making request #%d: %s %s\n", count, req.Method, req.URL.String())
	req.Header.Add("Authorization", "Bearer "+manualToken)
	return c.client.Do(req)
}

var httpClient = NewClient()

func FetchUsers() (map[string]User, error) {
	req, err := http.NewRequest("GET", testServerBase+"users", nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch users: %s", resp.Status)
	}

	var usersResp struct {
		Users map[string]string `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&usersResp); err != nil {
		return nil, err
	}

	users := make(map[string]User)
	for id, name := range usersResp.Users {
		users[id] = User{ID: id, Name: name}
	}

	return users, nil
}

func FetchUserPosts(userID string) ([]Post, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%susers/%s/posts", testServerBase, userID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch posts for user %s: %s", userID, resp.Status)
	}

	var postsResp struct {
		Posts []Post `json:"posts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postsResp); err != nil {
		return nil, err
	}

	return postsResp.Posts, nil
}

func FetchPostComments(postID int) ([]Comment, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%sposts/%d/comments", testServerBase, postID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch comments for post %d: %s", postID, resp.Status)
	}

	var commentsResp struct {
		Comments []Comment `json:"comments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commentsResp); err != nil {
		return nil, err
	}

	return commentsResp.Comments, nil
}

func TopUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	users, err := FetchUsers()
	if err != nil {
		http.Error(w, "Error fetching users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var wg sync.WaitGroup
	userPostCounts := make(map[string]int)
	mu := sync.Mutex{}

	for userID := range users {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			posts, err := FetchUserPosts(id)
			if err != nil {
				log.Printf("Error fetching posts for user %s: %v", id, err)
				return
			}

			mu.Lock()
			userPostCounts[id] = len(posts)
			mu.Unlock()
		}(userID)
	}
	wg.Wait()

	var topUsers []UserWithPostCount
	for id, count := range userPostCounts {
		topUsers = append(topUsers, UserWithPostCount{
			ID:        id,
			Name:      users[id].Name,
			PostCount: count,
		})
	}

	sort.Slice(topUsers, func(i, j int) bool {
		return topUsers[i].PostCount > topUsers[j].PostCount
	})

	if len(topUsers) > 5 {
		topUsers = topUsers[:5]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Users []UserWithPostCount `json:"users"`
	}{Users: topUsers})
}

func TopPostsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	postType := r.URL.Query().Get("type")
	if postType != "latest" && postType != "popular" {
		http.Error(w, "Invalid post type. Use 'latest' or 'popular'", http.StatusBadRequest)
		return
	}

	users, err := FetchUsers()
	if err != nil {
		http.Error(w, "Error fetching users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	posts := []Post{}
	for userID := range users {
		userPosts, err := FetchUserPosts(userID)
		if err != nil {
			log.Printf("Error fetching posts for user %s: %v", userID, err)
			continue
		}
		posts = append(posts, userPosts...)
	}

	var postsWithComments []PostWithCommentCount
	for _, post := range posts {
		comments, err := FetchPostComments(post.ID)
		if err != nil {
			log.Printf("Error fetching comments for post %d: %v", post.ID, err)
			continue
		}

		postsWithComments = append(postsWithComments, PostWithCommentCount{
			ID:           strconv.Itoa(post.ID),
			UserID:       strconv.Itoa(post.UserID),
			UserName:     users[strconv.Itoa(post.UserID)].Name,
			Content:      post.Content,
			CommentCount: len(comments),
		})
	}

	if postType == "popular" {
		sort.Slice(postsWithComments, func(i, j int) bool {
			return postsWithComments[i].CommentCount > postsWithComments[j].CommentCount
		})
	} else {
		sort.Slice(postsWithComments, func(i, j int) bool {
			return postsWithComments[i].ID > postsWithComments[j].ID
		})
	}

	if len(postsWithComments) > 5 {
		postsWithComments = postsWithComments[:5]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Posts []PostWithCommentCount `json:"posts"`
	}{Posts: postsWithComments})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s completed in %v", r.Method, r.URL.Path, time.Since(startTime))
	})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/users", TopUsersHandler)
	mux.HandleFunc("/posts", TopPostsHandler)

	handler := LoggingMiddleware(mux)

	port := ":8080"
	fmt.Printf("Server running on port %s\n", port)
	log.Fatal(http.ListenAndServe(port, handler))
}
