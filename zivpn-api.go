package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
	"flag" // Tambahkan flag untuk handling port
	"fmt"
)

const (
	ConfigFile = "/etc/zivpn/config.json"
	UserDB     = "/etc/zivpn/users.json"
	ApiKeyFile = "/etc/zivpn/apikey"
)

var AuthToken = "TUNNELOFFICIAL"

type Config struct {
	Listen string `json:"listen"`
	Cert   string `json:"cert"`
	Key    string `json:"key"`
	Obfs   string `json:"obfs"`
	Auth   struct {
		Mode   string   `json:"mode"`
		Config []string `json:"config"`
	} `json:"auth"`
}

type UserRequest struct {
	Password string `json:"password"`
	Days     int    `json:"days"`
	IpLimit  int    `json:"ip_limit"` // SEKARANG MENGENALI IP LIMIT
}

type UserStore struct {
	Password string `json:"password"`
	Expired  string `json:"expired"`
	Status   string `json:"status"`
	IpLimit  int    `json:"ip_limit"` // SEKARANG MENYIMPAN IP LIMIT
}

type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

var mutex = &sync.Mutex{}

func main() {
	// Parsing port dari command line flag
	portPtr := flag.Int("port", 8888, "Port to listen on")
	flag.Parse()

	if keyBytes, err := ioutil.ReadFile(ApiKeyFile); err == nil {
		AuthToken = strings.TrimSpace(string(keyBytes))
	}

	http.HandleFunc("/api/user/create", authMiddleware(createUser))
	http.HandleFunc("/api/user/delete", authMiddleware(deleteUser))
	http.HandleFunc("/api/user/renew", authMiddleware(renewUser))
	http.HandleFunc("/api/users", authMiddleware(listUsers))

	log.Printf("ZiVPN API Management started at :%d", *portPtr)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *portPtr), nil))
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-API-Key")
		if token != AuthToken {
			jsonResponse(w, http.StatusUnauthorized, false, "Unauthorized", nil)
			return
		}
		next(w, r)
	}
}

func jsonResponse(w http.ResponseWriter, status int, success bool, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{
		Success: success,
		Message: message,
		Data:    data,
	})
}

func createUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
		return
	}

	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	mutex.Lock()
	defer mutex.Unlock()

	config, err := loadConfig()
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca config ZiVPN", nil)
		return
	}

	config.Auth.Config = append(config.Auth.Config, req.Password)
	saveConfig(config)

	expDate := time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).Format("2006-01-02")
	users, _ := loadUsers()
	// MENYIMPAN DATA LENGKAP TERMASUK IP LIMIT
	users = append(users, UserStore{
		Password: req.Password, 
		Expired: expDate, 
		Status: "active",
		IpLimit: req.IpLimit,
	})
	saveUsers(users)

	exec.Command("systemctl", "restart", "zivpn.service").Run()

	jsonResponse(w, http.StatusOK, true, "User ZiVPN berhasil dibuat", map[string]interface{}{
		"password": req.Password,
		"expired":  expDate,
		"ip_limit": req.IpLimit,
	})
}

func deleteUser(w http.ResponseWriter, r *http.Request) {
	var req UserRequest
	json.NewDecoder(r.Body).Decode(&req)

	mutex.Lock()
	defer mutex.Unlock()

	config, _ := loadConfig()
	newAuth := []string{}
	for _, p := range config.Auth.Config {
		if p != req.Password {
			newAuth = append(newAuth, p)
		}
	}
	config.Auth.Config = newAuth
	saveConfig(config)

	users, _ := loadUsers()
	newUsers := []UserStore{}
	for _, u := range users {
		if u.Password != req.Password {
			newUsers = append(newUsers, u)
		}
	}
	saveUsers(newUsers)

	exec.Command("systemctl", "restart", "zivpn.service").Run()
	jsonResponse(w, http.StatusOK, true, "User berhasil dihapus", nil)
}

func renewUser(w http.ResponseWriter, r *http.Request) {
	var req UserRequest
	json.NewDecoder(r.Body).Decode(&req)

	mutex.Lock()
	defer mutex.Unlock()

	users, _ := loadUsers()
	var newExp string
	for i, u := range users {
		if u.Password == req.Password {
			currentExp, _ := time.Parse("2006-01-02", u.Expired)
			if currentExp.Before(time.Now()) {
				currentExp = time.Now()
			}
			newExp = currentExp.Add(time.Duration(req.Days) * 24 * time.Hour).Format("2006-01-02")
			users[i].Expired = newExp
			break
		}
	}
	saveUsers(users)
	jsonResponse(w, http.StatusOK, true, "User berhasil diperpanjang", map[string]string{"expired": newExp})
}

func listUsers(w http.ResponseWriter, r *http.Request) {
	users, _ := loadUsers()
	jsonResponse(w, http.StatusOK, true, "Daftar User ZiVPN", users)
}

func loadConfig() (Config, error) {
	var config Config
	file, err := ioutil.ReadFile(ConfigFile)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(file, &config)
	return config, err
}

func saveConfig(config Config) error {
	data, _ := json.MarshalIndent(config, "", "  ")
	return ioutil.WriteFile(ConfigFile, data, 0644)
}

func loadUsers() ([]UserStore, error) {
	var users []UserStore
	file, err := ioutil.ReadFile(UserDB)
	if err != nil {
		return users, nil
	}
	json.Unmarshal(file, &users)
	return users, nil
}

func saveUsers(users []UserStore) error {
	data, _ := json.MarshalIndent(users, "", "  ")
	return ioutil.WriteFile(UserDB, data, 0644)
}