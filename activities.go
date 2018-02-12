package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/boltdb/bolt"
)

type Activity struct {
	ID              int     `json:"id"`
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

	days := (365 * 2.5) / 30 // number of 30 day blocks in 2 1/2 years - go back to fall 2015
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

		startDate = startDate.AddDate(0, 0, -31)
		endDate = endDate.AddDate(0, 0, -31)
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
			// log.Printf("JSON: %s", prettyJSON(activity))
			seq, err := bk.NextSequence()
			if err != nil {
				return fmt.Errorf("could not get next sequence from bucket")
			}
			activity.ID = int(seq)
			activityBytes, err := json.Marshal(activity)
			if err != nil {
				return fmt.Errorf("could not marshal entry json: %v", err)
			}
			err = bk.Put(itob(activity.ID), activityBytes)
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

func loadActivities(accountID string, tradeType string) ([]Activity, error) {
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

			if tradeType == "all" || tradeType == act.Type {
				activities = append(activities, *act)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("could not load activities, %v", err)
	}
	log.Printf("Found %d activities.", len(activities))
	return activities, nil
}

func loadAllHistoricalSymbols(accountID string) (map[string]int, error) {
	symbols := make(map[string]int)
	activities, err := loadActivities(accountID, "Trades")
	if err != nil {
		return nil, err
	}

	for _, trade := range activities {
		symbols[trade.Symbol] = trade.SymbolID
	}

	return symbols, nil
}
