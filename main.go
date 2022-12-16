package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/xh3b4sd/budget/v3"
	"github.com/xh3b4sd/budget/v3/pkg/breaker"
	"github.com/xh3b4sd/framer"
)

const (
	apifmt = "https://api.stlouisfed.org/fred/series/observations?series_id=DTB1YR&api_key=%s&observation_start=%s&observation_end=%s&file_type=json"
	dayzer = "2020-12-01T00:00:00Z"
	reqlim = 50
	rewfil = "rates.csv"
)

type csvrow struct {
	Dat time.Time
	APR float64
}

type resstr struct {
	Count        int         `json:"count"`
	Observations []resstrdat `json:"observations"`
}

type resstrdat struct {
	Val string `json:"value"`
}

func main() {
	var err error

	var rea *os.File
	{
		rea, err = os.Open(rewfil)
		if err != nil {
			log.Fatal(err)
		}
	}

	var row [][]string
	{
		row, err = csv.NewReader(rea).ReadAll()
		if err != nil {
			log.Fatal(err)
		}
	}

	{
		rea.Close()
	}

	cur := map[time.Time]float64{}
	for _, x := range row[1:] {
		cur[mustim(x[0])] = musf64(x[1])
	}

	var sta time.Time
	{
		sta = mustim(dayzer)
	}

	var end time.Time
	{
		end = time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.UTC)
	}

	var bud budget.Interface
	{
		bud = breaker.Default()
	}

	var fra *framer.Framer
	{
		fra = framer.New(framer.Config{
			Sta: sta,
			End: end,
			Len: 24 * time.Hour,
		})
	}

	var cou int
	des := map[time.Time]float64{}
	for _, x := range fra.List() {
		f64, exi := cur[x.Sta]
		if exi {
			{
				// log.Printf("setting cached interest rates for %s\n", x.Sta)
			}

			{
				des[x.Sta] = f64
			}
		} else if cou < reqlim {
			{
				cou++
			}

			{
				log.Printf("filling remote interest rates for %s\n", x.Sta)
			}

			var act func() error
			{
				act = func() error {
					var f64 float64
					{
						f64 = musapi(x.Sta)
					}

					if f64 == -1 {
						f64 = des[x.Sta.Add(-24*time.Hour)]
					}

					{
						des[x.Sta] = f64
					}

					return nil
				}
			}

			{
				err = bud.Execute(act)
				if budget.IsCancel(err) {
					break
				} else if budget.IsPassed(err) {
					break
				} else if err != nil {
					log.Fatal(err)
				}
			}

			{
				time.Sleep(200 * time.Millisecond)
			}
		}
	}

	var lis []csvrow
	for k, v := range des {
		lis = append(lis, csvrow{Dat: k, APR: v})
	}

	{
		sort.SliceStable(lis, func(i, j int) bool { return lis[i].Dat.Before(lis[j].Dat) })
	}

	var res [][]string
	{
		res = append(res, []string{"date", "rate"})
	}

	for _, x := range lis {
		res = append(res, []string{x.Dat.Format(time.RFC3339), fmt.Sprintf("%.2f", x.APR)})
	}

	var wri *os.File
	{
		wri, err = os.OpenFile(rewfil, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)
		if err != nil {
			log.Fatal(err)
		}
	}

	{
		defer wri.Close()
	}

	{
		err = csv.NewWriter(wri).WriteAll(res)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func musapi(des time.Time) float64 {
	var err error

	var key string
	{
		key = muskey()
	}

	var str string
	{
		str = des.Format("2006-01-02")
	}

	var cli *http.Client
	{
		cli = &http.Client{Timeout: 10 * time.Second}
	}

	var res *http.Response
	{
		u := fmt.Sprintf(apifmt, key, str, str)

		res, err = cli.Get(u)
		if err != nil {
			log.Fatal(err)
		}
	}

	{
		defer res.Body.Close()
	}

	var byt []byte
	{
		byt, err = io.ReadAll(res.Body)
		if err != nil {
			log.Fatal(err)
		}
	}

	var dat resstr
	{
		err = json.Unmarshal(byt, &dat)
		if err != nil {
			log.Fatal(err)
		}
	}

	if dat.Count != 1 {
		return -1
	}

	if len(dat.Observations) != 1 {
		return -1
	}

	// It appears that the API sometimes returns "." for values if it doesn't
	// have a number to return. In that case we set the desired interest rate to
	// 0.0%.
	//
	//     https://github.com/plkrueger/CommonLispFred/blob/master/fred.lisp#L972-L977
	//
	if dat.Observations[0].Val == "." {
		return 0
	}

	return musf64(dat.Observations[0].Val)
}

func musf64(str string) float64 {
	f64, err := strconv.ParseFloat(str, 64)
	if err != nil {
		log.Fatal(err)
	}

	return f64
}

func muskey() string {
	key := os.Getenv("FRED_API_KEY")
	if key == "" {
		panic("${FRED_API_KEY} must not be empty")
	}

	return key
}

func mustim(str string) time.Time {
	tim, err := time.Parse(time.RFC3339, str)
	if err != nil {
		log.Fatal(err)
	}

	return tim
}
