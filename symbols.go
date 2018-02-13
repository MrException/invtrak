package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
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

type Candles struct {
	Candles []Candle `json:"candles"`
}

func (c Candles) Len() int {
	return len(c.Candles)
}

func (list Candles) XY(i int) (float64, float64) {
	c := list.Candles[i]
	X, err := c.dayFloat()
	if err != nil {
		panic(err)
	}
	Y := c.Close
	return X, Y
}

func (c Candle) dayFloat() (float64, error) {
	d, err := time.Parse(time.RFC3339Nano, c.Start)
	if err != nil {
		return 0, fmt.Errorf("error parsing date for candle %s, %v", c.Start, err)
	}
	// dStr := fmt.Sprintf("%d%02d%02d", d.Year(), d.Month(), d.Day())
	// fmt.Println(dStr)
	// dFlt, _ := strconv.ParseFloat(dStr, 64)
	// return dFlt, nil
	return float64(d.Unix()), nil
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

	data := &Candles{}
	err = json.Unmarshal(res, data)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %s", err)
	}

	// for i, candle := range data.Candles {
	// log.Printf("Candle %s: %s", string(i), candle)
	// }

	drawPlot(symbolID, *data)

	// log.Printf("%+v\n", accounts)

	return nil
}

func drawPlot(symbolID string, candles Candles) error {
	p, err := plot.New()
	if err != nil {
		return fmt.Errorf("error constructing plot, %v", err)
	}

	p.X.Tick.Marker = plot.TimeTicks{}

	p.Title.Text = "Thing"
	p.X.Label.Text = "Date"
	p.Y.Label.Text = "Price"

	err = plotutil.AddLinePoints(p, symbolID, candles)
	if err != nil {
		return fmt.Errorf("error plotting points, %v", err)
	}

	if err := p.Save(10*vg.Inch, 10*vg.Inch, "tmp/plot.svg"); err != nil {
		return fmt.Errorf("error creating the plot image, %v", err)
	}

	return nil
}
