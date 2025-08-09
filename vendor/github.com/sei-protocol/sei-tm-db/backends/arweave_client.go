package backends

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strconv"
)

type TransactionOffset struct {
	Size   string `json:"size"`
	Offset string `json:"offset"`
}

type TransactionChunk struct {
	Chunk    string `json:"chunk"`
	DataPath string `json:"data_path"`
	TxPath   string `json:"tx_path"`
}

type Client struct {
	client *http.Client
	url    string
}

func NewClient(nodeUrl string, proxyUrl ...string) *Client {
	httpClient := http.DefaultClient
	// if exist proxy url
	if len(proxyUrl) > 0 {
		pUrl := proxyUrl[0]
		proxyUrl, err := url.Parse(pUrl)
		if err != nil {
			panic(err)
		}
		tr := &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
		httpClient = &http.Client{Transport: tr}
	}

	return &Client{client: httpClient, url: nodeUrl}
}

func (c *Client) getTransactionOffset(id string) (*TransactionOffset, error) {
	_path := fmt.Sprintf("tx/%s/offset", id)
	body, statusCode, err := c.httpGet(_path)
	if statusCode != 200 {
		return nil, errors.New("not found tx offset")
	}
	if err != nil {
		return nil, err
	}
	txOffset := &TransactionOffset{}
	if err := json.Unmarshal(body, txOffset); err != nil {
		return nil, err
	}
	return txOffset, nil
}

func (c *Client) httpGet(_path string) (body []byte, statusCode int, err error) {
	u, err := url.Parse(c.url)
	if err != nil {
		return
	}

	u.Path = path.Join(u.Path, _path)

	resp, err := c.client.Get(u.String())
	if err != nil {
		return
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode
	body, err = ioutil.ReadAll(resp.Body)
	return
}

func (c *Client) DownloadChunkData(id string) ([]byte, error) {
	offsetResponse, err := c.getTransactionOffset(id)
	if err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(offsetResponse.Size, 10, 64)
	if err != nil {
		return nil, err
	}
	endOffset, err := strconv.ParseInt(offsetResponse.Offset, 10, 64)
	if err != nil {
		return nil, err
	}
	startOffset := endOffset - size + 1
	data := make([]byte, 0, size)
	for i := 0; int64(i)+startOffset < endOffset; {
		chunkData, err := c.getChunkData(int64(i) + startOffset)
		if err != nil {
			return nil, err
		}
		data = append(data, chunkData...)
		i += len(chunkData)
	}
	return data, nil
}

func (c *Client) getChunkData(offset int64) ([]byte, error) {
	chunk, err := c.getChunk(offset)
	if err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(chunk.Chunk)
}

func (c *Client) getChunk(offset int64) (*TransactionChunk, error) {
	_path := "chunk/" + strconv.FormatInt(offset, 10)
	body, statusCode, err := c.httpGet(_path)
	if statusCode != 200 {
		return nil, errors.New("not found chunk data")
	}
	if err != nil {
		return nil, err
	}
	txChunk := &TransactionChunk{}
	if err := json.Unmarshal(body, txChunk); err != nil {
		return nil, err
	}
	return txChunk, nil
}
