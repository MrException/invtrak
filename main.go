package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/boltdb/bolt"
)

type cliConfig struct {
	command string
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

type Activity struct {
	TradeDate       string  `json:"tradeDate"`
	TransactionDate string  `json:"transactionDate"`
	SettlementDate  string  `json:"settlementDate"`
	Action          string  `json:"action"`
	Symbol          string  `json:"symbol"`
	SymbolID        int     `json:"symbolId"`
	Description     string  `json:"description"`
	Currency        string  `json:"currency"`
	Quantity        int     `json:"quantity"`
	Price           float64 `json:"price"`
	GrossAmount     float64 `json:"grossAmount"`
	Commission      float64 `json:"commission"`
	NetAmount       float64 `json:"netAmount"`
	Type            string  `json:"type"`
}

func (a Activity) String() string {
	return prettyJSON(a)
}

type ActivitiesReq struct {
	Activities []Activity `json:"activities"`
}

var conf *cliConfig
var db *bolt.DB
var token *authToken

func init() {
	conf = &cliConfig{}
	flag.StringVar(&conf.command, "command", "list-accounts", "Command to run: init, list-accounts, activities")
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
		activities, err = loadActivities(arg)
		if err == nil {
			if len(activities) == 0 {
				log.Printf("No activities found for account %s. Try refresh-activites.", arg)
			} else {
				log.Printf("Activities for account %s: %s", arg, activities)
			}
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

func refreshAllActivities() error {
	accounts, err := loadAccounts()
	if err != nil {
		return err
	}

	for _, account := range accounts {
		err = refreshActivities(account.Number)
		if err != nil {
			return err
		}
	}

	return nil

}

func refreshActivities(accountID string) error {
	return requestActivities(accountID)
}

func requestActivities(accountID string) error {
	log.Printf("Requesting Activities.")
	// start with the most recent 30 days
	startDate := time.Now().AddDate(0, 0, -30)
	endDate := time.Now()

	days := (365 * 2.25) / 30 // number of 30 day blocks in 2 1/4 years - go back to fall 2015
	for i := 0; i <= int(days); i++ {
		url := fmt.Sprintf("%sv1/accounts/%s/activities?startTime=%s&endTime=%s", token.APIServer, accountID, startDate.Format(time.RFC3339), endDate.Format(time.RFC3339))
		res, err := doReq(url, true)
		if err != nil {
			return fmt.Errorf("error requesting accounts, %v", err)
		}
		_, err = saveActivities(res, accountID)
		if err != nil {
			return fmt.Errorf("error saving activities, %v", err)
		}
		// log.Printf("Response: %s\n", string(res))

		startDate = startDate.AddDate(0, 0, -30)
		endDate = endDate.AddDate(0, 0, -30)
	}

	// log.Printf("%+v\n", accounts)

	return nil
}

func saveActivities(body []byte, accountID string) ([]Activity, error) {
	log.Println("Saving Activities.")

	activities := &ActivitiesReq{}
	err := json.Unmarshal(body, activities)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %s", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		bkName := fmt.Sprintf("ACTIVITIES-%s", accountID)
		bk, err := tx.CreateBucketIfNotExists([]byte(bkName))
		if err != nil {
			return fmt.Errorf("couldn't get/create %s bucket, %v", bkName, err)
		}

		for _, activity := range activities.Activities {
			log.Printf("JSON: %s", activity)
			activityBytes, err := json.Marshal(activity)
			if err != nil {
				return fmt.Errorf("could not marshal entry json: %v", err)
			}
			err = bk.Put([]byte(activity.TradeDate), activityBytes)
			if err != nil {
				return fmt.Errorf("could not insert activity: %v", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("could not save activities, %v", err)
	}
	return activities.Activities, nil
}

func loadActivities(accountID string) ([]Activity, error) {
	log.Println("Loading Activities.")
	activities := make([]Activity, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bkName := fmt.Sprintf("ACTIVITIES-%s", accountID)
		bk := tx.Bucket([]byte(bkName))
		if bk == nil {
			return fmt.Errorf("couldn't get %s bucket", bkName)
		}

		c := bk.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			act := &Activity{}
			err := json.Unmarshal(v, act)
			if err != nil {
				return fmt.Errorf("could not unmarshal activity: %v", err)
			}

			activities = append(activities, *act)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("could not load activities, %v", err)
	}
	log.Printf("Found %d activities.", len(activities))
	return activities, nil
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
