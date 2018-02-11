package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/boltdb/bolt"
)

type cliConfig struct {
	command   string
	tradeType string
}

type authToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	APIServer    string `json:"api_server"`
}

func (t authToken) String() string {
	return prettyJSON(t)
}

type account struct {
	Type              string `json:"type"`
	Number            string `json:"number"`
	Status            string `json:"status"`
	IsPrimary         bool   `json:"isPrimary"`
	IsBilling         bool   `json:"isBilling"`
	ClientAccountType string `json:"clientAccountType"`
}

func (a account) String() string {
	return prettyJSON(a)
}

type accountReq struct {
	Accounts []account `json:"accounts"`
	UserID   int       `json:"userId"`
}

var conf *cliConfig
var db *bolt.DB
var token *authToken

func init() {
	conf = &cliConfig{}
	flag.StringVar(&conf.command, "command", "list-accounts", "Command to run: init, list-accounts, activities")
	flag.StringVar(&conf.tradeType, "trade-type", "all", "Type of trade, default 'all'")
}

func main() {
	flag.Parse()

	err := setupDB()
	if err != nil {
		log.Fatal(err)
	}

	err = initToken()
	if err != nil {
		log.Fatal(err)
	}

	switch conf.command {
	case "setup":
		err = setup()
	case "list-accounts":
		var accounts []account
		accounts, err = loadAccounts()
		if err == nil {
			log.Printf("Loaded accounts: %v", accounts)
		}

	case "refresh-activities":
		arg := flag.Arg(0)
		if len(arg) == 0 {
			err = fmt.Errorf("refresh-activities command requires an accountID argument or 'all'")
			break
		}
		if arg == "all" {
			err = refreshAllActivities()
		} else {
			err = refreshActivities(arg)
		}

	case "list-activities":
		arg := flag.Arg(0)
		if len(arg) == 0 {
			err = fmt.Errorf("list-activities command requires an accountID argument")
			break
		}
		var activities []Activity
		activities, err = loadActivities(arg, conf.tradeType)
		if err == nil {
			if len(activities) == 0 {
				log.Printf("No activities found for account %s. Try refresh-activites.", arg)
			} else {
				log.Printf("Activities for account %s: %s", arg, activities)
			}
		}

	case "list-symbols":
		arg := flag.Arg(0)
		if len(arg) == 0 {
			err = fmt.Errorf("list-activities command requires an accountID argument")
			break
		}
		var symbols []string
		symbols, err = loadAllHistoricalSymbols(arg)
		if err == nil {
			log.Printf("All symbols traded in account %s: %s", arg, symbols)
		}

	default:
		err = fmt.Errorf("invalid command: %s", conf.command)
	}

	db.Close()

	if err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}

func initToken() error {
	err := loadToken()
	if err != nil {
		refreshTokenStr, found := os.LookupEnv("REFRESH_TOKEN")
		if !found {
			return fmt.Errorf("no token saved in DB, and no REFRESH_TOKEN env var set")
		}
		err = requestToken(refreshTokenStr)

		if err != nil {
			return err
		}
	} else {
		err = requestToken(token.RefreshToken)
		if err != nil {
			return err
		}
	}
	return nil
}

func setup() error {
	accounts, err := requestAccounts()
	if err != nil {
		return err
	}

	err = saveAccounts(accounts)
	if err != nil {
		return err
	}
	return nil
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
			return fmt.Errorf("could not create ROOT bucket: %v", err)
		}
		_, err = tx.CreateBucketIfNotExists([]byte("ACCOUNTS"))
		if err != nil {
			return fmt.Errorf("could not create ACCOUNTS bucket: %v", err)
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
	log.Println("Saving Token.")
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
	return nil
}

func loadToken() error {
	log.Println("Loading Token.")
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
	return nil
}

func requestToken(refreshTokenStr string) error {
	// todo: save the last use of the token, only do a request if needed using authToken.ExpiresIn
	log.Println("Requesting new token.")
	url := fmt.Sprintf("https://login.questrade.com/oauth2/token?grant_type=refresh_token&refresh_token=%s", refreshTokenStr)

	body, err := doReq(url, false)
	if err != nil {
		return fmt.Errorf("error requesting token, %v", err)
	}

	token = &authToken{}
	err = json.Unmarshal(body, token)
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

func requestAccounts() (*accountReq, error) {
	log.Printf("Requesting accounts.")
	url := fmt.Sprintf("%sv1/accounts", token.APIServer)

	body, err := doReq(url, true)
	if err != nil {
		return nil, fmt.Errorf("error requesting accounts, %v", err)
	}

	accounts := &accountReq{}
	err = json.Unmarshal(body, accounts)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %s", err)
	}
	// log.Printf("%+v\n", accounts)

	return accounts, nil
}

func saveAccounts(accounts *accountReq) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("ACCOUNTS"))
		if bucket == nil {
			return fmt.Errorf("couldn't get ACCOUNTS bucket")
		}

		for _, account := range accounts.Accounts {
			accountBytes, err := json.Marshal(account)
			if err != nil {
				return fmt.Errorf("could not marshal entry json: %v", err)
			}
			err = bucket.Put([]byte(account.Number), accountBytes)
			if err != nil {
				return fmt.Errorf("could not insert account: %v", err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("could not save accounts, %v", err)
	}
	log.Println("Successfully saved Accounts.")
	return nil
}

func loadAccounts() ([]account, error) {
	log.Println("Loading Accounts.")
	accounts := make([]account, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket([]byte("ACCOUNTS"))
		if bk == nil {
			return fmt.Errorf("couldn't get ACCOUNTS bucket")
		}

		c := bk.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			acc := &account{}
			err := json.Unmarshal(v, acc)
			if err != nil {
				return fmt.Errorf("could not unmarshal account: %v", err)
			}

			accounts = append(accounts, *acc)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("could not load accounts, %v", err)
	}
	return accounts, nil
}

func prettyJSON(obj interface{}) string {
	out, _ := json.MarshalIndent(obj, "", "  ")
	return string(out)
}

func doReq(url string, addAuth bool) ([]byte, error) {
	log.Printf("Sending GET to %s", url)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("err creating request, %v", err)
	}

	if addAuth {
		auth := fmt.Sprintf("Bearer %s", token.AccessToken)
		request.Header.Set("Authorization", auth)
	}

	client := &http.Client{}
	res, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("error performing request, %v", err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body: %s", err)
	}

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("error return code: %d", res.StatusCode)
	}

	return body, nil
}

// itob returns an 8-byte big endian representation of v.
func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}
