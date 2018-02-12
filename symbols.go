package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type Candle struct {
	Start  string  `json:"start"`
	End    string  `json:"end"`
	Low    float64 `json:"low"`
	High   float64 `json:"high"`
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	Volume int     `json:"volume"`
}

func (c Candle) String() string {
	return prettyJSON(c)
}

type candlestickReq struct {
	Candles []Candle `json:"candles"`
}

func requestCandles(symbolID string) error {
	log.Printf("Requesting Candles.")
	// start with the most recent 2.5 years
	days := -1 * (365 * 2.5) // number of days in 2 1/2 years - go back to fall 2015
	startDate := time.Now().AddDate(0, 0, int(days))
	endDate := time.Now()

	url := fmt.Sprintf("%sv1/markets/candles/%s?interval=OneDay&startTime=%s&endTime=%s", token.APIServer, symbolID, startDate.Format(time.RFC3339), endDate.Format(time.RFC3339))
	res, err := doReq(url, true)
	if err != nil {
		return fmt.Errorf("error requesting candles, %v", err)
	}

	data := &candlestickReq{}
	err = json.Unmarshal(res, data)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %s", err)
	}

	for i, candle := range data.Candles {
		log.Printf("Candle %s: %s", string(i), candle)
	}

	// log.Printf("%+v\n", accounts)

	return nil
}
