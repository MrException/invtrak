package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/boltdb/bolt"
)

type authToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	APIServer    string `json:"api_server"`
}

type account struct {
	Type              string `json:"type"`
	Number            string `json:"number"`
	Status            string `json:"status"`
	IsPrimary         bool   `json:"isPrimary"`
	IsBilling         bool   `json:"isBilling"`
	ClientAccountType string `json:"clientAccountType"`
}

type accountReq struct {
	Accounts []account `json:"accounts"`
	UserID   int       `json:"userId"`
}

var db *bolt.DB

var token *authToken

func main() {
	err := setupDB()
	if err != nil {
		log.Fatal(err)
	}

	err = loadToken()
	if err != nil {
		refreshTokenStr, found := os.LookupEnv("REFRESH_TOKEN")
		if !found {
			log.Fatal("No token saved in DB, and no REFRESH_TOKEN env var set.")
		}
		err = requestToken(refreshTokenStr)

		if err != nil {
			log.Fatal(err)
		}
	} else {
		err = refreshToken()
		if err != nil {
			log.Fatal(err)
		}
	}

	err = readAccounts()
	if err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}

func setupDB() error {
	var err error
	db, err = bolt.Open("test.db", 0600, nil)
	if err != nil {
		return fmt.Errorf("could not open db, %v", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("ROOT"))
		if err != nil {
			return fmt.Errorf("could not create root bucket: %v", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not set up buckets, %v", err)
	}
	log.Println("DB Setup Done")
	return nil
}

func saveToken() error {
	tokenBytes, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("could not marshal entry json: %v", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		err := tx.Bucket([]byte("ROOT")).Put([]byte("TOKEN"), tokenBytes)
		if err != nil {
			return fmt.Errorf("could not insert token: %v", err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("could not save token, %v", err)
	}
	log.Println("Successfully saved Token.")
	return nil
}

func loadToken() error {
	err := db.View(func(tx *bolt.Tx) error {
		tokenStr := tx.Bucket([]byte("ROOT")).Get([]byte("TOKEN"))
		if tokenStr == nil {
			return fmt.Errorf("no token found")
		}
		// log.Printf("Loaded token from db: %v", string(tokenStr))

		token = &authToken{}
		err := json.Unmarshal(tokenStr, token)
		if err != nil {
			return fmt.Errorf("could not unmarshal token: %v", err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("could not load token, %v", err)
	}
	log.Println("Successfully loaded Token.")
	return nil
}

func requestToken(refreshTokenStr string) error {
	log.Println("Requesting new token.")
	refreshURL := fmt.Sprintf("https://login.questrade.com/oauth2/token?grant_type=refresh_token&refresh_token=%s", refreshTokenStr)

	// log.Println(refreshURL)
	res, err := http.Get(refreshURL)
	if err != nil {
		return fmt.Errorf("error from Get: %q", err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error reading body: %s", err)
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("error return code: %d", res.StatusCode)
	}

	token = &authToken{}
	err = json.Unmarshal([]byte(body), token)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %s", err)
	}
	// log.Printf("%+v\n", token)

	err = saveToken()
	if err != nil {
		return fmt.Errorf("error saving token: %sn", err)
	}

	return nil
}

func refreshToken() error {
	log.Println("Refreshing saved token.")
	return requestToken(token.RefreshToken)
}

func readAccounts() error {
	url := fmt.Sprintf("%sv1/accounts", token.APIServer)
	log.Printf("Sending GET to %s", url)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("err creating request, %v", err)
	}

	auth := fmt.Sprintf("Bearer %s", token.AccessToken)
	request.Header.Set("Authorization", auth)
	client := &http.Client{}
	res, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("error getting accounts, %v", err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("error reading body: %s", err)
	}
	// log.Printf("Response: %s\n", string(body))

	if res.StatusCode != 200 {
		return fmt.Errorf("error return code: %d", res.StatusCode)
	}

	accounts := &accountReq{}
	err = json.Unmarshal([]byte(body), accounts)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %s", err)
	}
	log.Printf("%+v\n", accounts)

	return nil
}
