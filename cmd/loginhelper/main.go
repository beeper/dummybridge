package main

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"go.mau.fi/util/exerrors"
)

//go:embed pages/*.html
var pages embed.FS

var waiters = make(map[string]chan map[string]string)
var inFlightRequests = make(map[string]int)
var lock sync.RWMutex

func addWaiter(ip, id string, waiter chan map[string]string) bool {
	lock.Lock()
	defer lock.Unlock()
	if inFlightRequests[ip] > 5 {
		return false
	}
	inFlightRequests[ip]++
	waiters[id] = waiter
	return true
}

func callWaiter(id string, data map[string]string) bool {
	lock.RLock()
	defer lock.RUnlock()
	waiter, ok := waiters[id]
	if !ok {
		return false
	}
	waiter <- data
	return true
}

type errorResp struct {
	Error string `json:"error"`
}

func response(w http.ResponseWriter, status int, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func postWait(w http.ResponseWriter, r *http.Request) {
	ip := r.Header.Get("X-Forwarded-For")
	waiter := make(chan map[string]string)
	reqID := r.PathValue("id")
	if !addWaiter(ip, reqID, waiter) {
		response(w, http.StatusTooManyRequests, errorResp{"Too many in-flight requests"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	defer func() {
		lock.Lock()
		defer lock.Unlock()
		inFlightRequests[ip]--
		delete(waiters, reqID)
	}()
	select {
	case <-ctx.Done():
		response(w, http.StatusTooManyRequests, errorResp{"Wait timeout"})
	case resp := <-waiter:
		response(w, http.StatusOK, resp)
	}
}

func postSubmit(w http.ResponseWriter, r *http.Request) {
	reqID := r.PathValue("id")
	var input map[string]string
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		response(w, http.StatusBadRequest, errorResp{"Invalid JSON"})
		return
	}
	if callWaiter(reqID, input) {
		response(w, http.StatusOK, struct{}{})
	} else {
		response(w, http.StatusNotFound, errorResp{"Request not found"})
	}
}

func postSetCookies(w http.ResponseWriter, r *http.Request) {
	var input map[string]string
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		response(w, http.StatusBadRequest, errorResp{"Invalid JSON"})
		return
	}
	for k, v := range input {
		http.SetCookie(w, &http.Cookie{
			Name:     k,
			Value:    v,
			Expires:  time.Now().Add(1 * time.Hour),
			HttpOnly: true,
			Secure:   true,
		})
	}
	response(w, http.StatusOK, struct{}{})
}

func main() {
	http.Handle("/pages/", http.FileServerFS(pages))
	http.HandleFunc("POST /api/daw_wait/{id}", postWait)
	http.HandleFunc("POST /api/daw_submit/{id}", postSubmit)
	http.HandleFunc("POST /api/set_cookies", postSetCookies)
	exerrors.PanicIfNotNil(http.ListenAndServe(":8080", nil))
}
