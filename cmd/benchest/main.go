package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/application-research/estuary/util"
	cli "github.com/urfave/cli/v2"
)

func main() {
	app := cli.NewApp()

	app.Name = "benchest"
	app.Commands = []*cli.Command{
		benchAddFileCmd,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getFile(cctx *cli.Context) (io.ReadCloser, string, error) {
	buf := make([]byte, 1024*1024)
	rand.Read(buf)

	return io.NopCloser(bytes.NewReader(buf)), fmt.Sprintf("goodfile-%x", buf[:4]), nil
}

type benchResult struct {
	BenchStart      time.Time
	FileCID         string
	AddFileRespTime time.Duration
	AddFileTime     time.Duration

	FetchStats *fetchStats
	IpfsCheck  *checkResp
}

var benchAddFileCmd = &cli.Command{
	Name: "add-file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "host",
			Value: "api.estuary.tech",
		},
	},
	Action: func(cctx *cli.Context) error {
		estToken := os.Getenv("ESTUARY_TOKEN")
		if estToken == "" {
			return fmt.Errorf("no estuary token found")
		}

		fi, name, err := getFile(cctx)
		if err != nil {
			return err
		}

		defer fi.Close()

		host := cctx.String("host")

		buf := new(bytes.Buffer)
		mw := multipart.NewWriter(buf)
		part, err := mw.CreateFormFile("data", name)
		if err != nil {
			return err
		}
		io.Copy(part, fi)
		mw.Close()

		req, err := http.NewRequest("POST", fmt.Sprintf("https://%s/content/add", host), buf)
		if err != nil {
			return err
		}

		req.Header.Add("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+estToken)

		addReqStart := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		addRespAt := time.Now()

		if resp.StatusCode != 200 {
			var m map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
				fmt.Println(err)
			}
			fmt.Println("error body: ", m)
			return fmt.Errorf("got invalid status code: %d", resp.StatusCode)
		}

		var rbody util.AddFileResponse
		if err := json.NewDecoder(resp.Body).Decode(&rbody); err != nil {
			return err
		}
		readBodyTime := time.Now()

		fmt.Println("file added, cid: ", rbody.Cid)

		chk := make(chan *checkResp)
		go func() {
			if len(rbody.Providers) == 0 {
				chk <- &checkResp{
					CheckRequestError: "no addresses back from add response",
				}
				return
			}

			addr := rbody.Providers[0]
			for _, a := range rbody.Providers {
				if !strings.Contains(a, "127.0.0.1") {
					addr = a
				}
			}

			chk <- ipfsCheck(rbody.Cid, addr)
		}()

		st, err := benchFetch(rbody.Cid)
		if err != nil {
			return err
		}

		chkresp := <-chk

		outstats := &benchResult{
			BenchStart:      addReqStart,
			FileCID:         rbody.Cid,
			AddFileRespTime: addRespAt.Sub(addReqStart),
			AddFileTime:     readBodyTime.Sub(addReqStart),

			FetchStats: st,
			IpfsCheck:  chkresp,
		}
		b, err := json.MarshalIndent(outstats, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))

		return nil
	},
}

type fetchStats struct {
	RequestStart time.Time
	GatewayURL   string

	GatewayHost string
	StatusCode  int

	ResponseTime      time.Duration
	TimeToFirstByte   time.Duration
	TotalTransferTime time.Duration
	TotalElapsed      time.Duration
}

func benchFetch(c string) (*fetchStats, error) {
	url := "https://dweb.link/ipfs/" + c
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	afterDo := time.Now()
	if err != nil {
		return nil, fmt.Errorf("response errored: %s", err)
	}

	gwayhost := resp.Header.Get("x-ipfs-gateway-host")

	status := resp.StatusCode

	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)

	_, err = br.Peek(1)
	if err != nil {
		return nil, fmt.Errorf("peeking first byte failed: %w", err)
	}
	firstByteAt := time.Now()

	io.Copy(io.Discard, br)
	endTime := time.Now()

	return &fetchStats{
		RequestStart: start,
		GatewayURL:   url,
		StatusCode:   status,

		GatewayHost: gwayhost,

		ResponseTime:      afterDo.Sub(start),
		TimeToFirstByte:   firstByteAt.Sub(start),
		TotalTransferTime: endTime.Sub(firstByteAt),
		TotalElapsed:      endTime.Sub(start),
	}, nil
}

type checkResp struct {
	CheckTook                time.Duration
	CheckRequestError        string
	ConnectionError          string
	PeerFoundInDHT           map[string]int
	CidInDHT                 bool
	DataAvailableOverBitswap struct {
		Found     bool
		Responded bool
		Error     string
	}
}

func ipfsCheck(c string, maddr string) *checkResp {
	start := time.Now()
	resp, err := http.DefaultClient.Get(fmt.Sprintf("https://ipfs-check-backend.ipfs.io/?cid=%s&multiaddr=%s", c, maddr))
	if err != nil {
		return &checkResp{
			CheckTook:         time.Since(start),
			CheckRequestError: err.Error(),
		}
	}

	defer resp.Body.Close()

	var out checkResp
	out.CheckTook = time.Since(start)
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return &checkResp{
			CheckTook:         time.Since(start),
			CheckRequestError: err.Error(),
		}
	}

	return &out
}