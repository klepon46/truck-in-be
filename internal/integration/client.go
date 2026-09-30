package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"truckin-be/internal/movement"
)

var ErrNotFound = errors.New("external record not found")

type Config struct {
	LambungBaseURL  string
	LambungToken    string
	SAOSBaseURL     string
	SAOSToken       string
	WorkshopBaseURL string
	WorkshopToken   string
	DriverBaseURL   string
	DriverToken     string
	ConnectTimeout  time.Duration
	RequestTimeout  time.Duration
}

type Client struct {
	config Config
	http   *http.Client
}

func NewClient(config Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: config.ConnectTimeout}).DialContext
	return &Client{config: config, http: &http.Client{Timeout: config.RequestTimeout, Transport: transport}}
}

func (c *Client) Workshop(ctx context.Context, number string) (movement.Document, error) {
	var response struct {
		Data struct {
			Number     string `json:"work_order_number"`
			DriverName string `json:"driver_name"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, c.config.WorkshopBaseURL, c.config.WorkshopToken, "/internal/v1/work-orders/by-wo?wo-number="+url.QueryEscape(number), &response); err != nil {
		return movement.Document{}, err
	}
	if response.Data.Number != number || strings.TrimSpace(response.Data.DriverName) == "" {
		return movement.Document{}, ErrNotFound
	}
	return movement.Document{DriverName: strings.TrimSpace(response.Data.DriverName)}, nil
}

func (c *Client) SPP(ctx context.Context, number string) (movement.Document, error) {
	var response struct {
		Data []struct {
			Number       string `json:"trans_no"`
			DriverName   string `json:"driver_name"`
			CustomerID   string `json:"customer_id"`
			CustomerName string `json:"customer_name"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, c.config.SAOSBaseURL, c.config.SAOSToken, "/api/v1/loading-orders?transNo="+url.QueryEscape(number), &response); err != nil {
		return movement.Document{}, err
	}
	for _, record := range response.Data {
		if record.Number == number && strings.TrimSpace(record.DriverName) != "" {
			return movement.Document{DriverName: strings.TrimSpace(record.DriverName), CustomerID: strings.TrimSpace(record.CustomerID), CustomerName: strings.TrimSpace(record.CustomerName)}, nil
		}
	}
	return movement.Document{}, ErrNotFound
}

func (c *Client) Driver(ctx context.Context, id int64, name string) error {
	var response struct {
		Data []struct {
			ID         json.RawMessage `json:"id"`
			DriverName string          `json:"driverName"`
			ResignDate *string         `json:"resignDate"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, c.config.DriverBaseURL, c.config.DriverToken, "/active-drivers?name="+url.QueryEscape(name), &response); err != nil {
		return err
	}
	for _, driver := range response.Data {
		driverID, err := parseID(driver.ID)
		if err == nil && driverID == id && strings.EqualFold(strings.TrimSpace(driver.DriverName), strings.TrimSpace(name)) && driver.ResignDate == nil {
			return nil
		}
	}
	return ErrNotFound
}

type Unit struct {
	ID        int64  `json:"id"`
	NoLambung string `json:"noLambung"`
	NoPolisi  string `json:"noPolisi"`
}

type UnitPage struct {
	Items      []Unit  `json:"items"`
	NextCursor *string `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}

type unitPageEnvelope struct {
	Data *struct {
		Items      *[]Unit         `json:"items"`
		NextCursor json.RawMessage `json:"nextCursor"`
		HasMore    *bool           `json:"hasMore"`
	} `json:"data"`
}

func (c *Client) Units(ctx context.Context, cursor string, size int) (UnitPage, error) {
	var response unitPageEnvelope
	query := url.Values{}
	query.Set("limit", strconv.Itoa(size))
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	path := "/lambung/v1/internal/unit-snapshots?" + query.Encode()
	if err := c.getJSON(ctx, c.config.LambungBaseURL, c.config.LambungToken, path, &response); err != nil {
		return UnitPage{}, err
	}
	if response.Data == nil || response.Data.Items == nil || response.Data.NextCursor == nil || response.Data.HasMore == nil {
		return UnitPage{}, errors.New("invalid Unit Lambung cursor page")
	}

	var nextCursor *string
	if err := json.Unmarshal(response.Data.NextCursor, &nextCursor); err != nil {
		return UnitPage{}, errors.New("invalid Unit Lambung cursor page")
	}
	page := UnitPage{Items: *response.Data.Items, NextCursor: nextCursor, HasMore: *response.Data.HasMore}
	if page.HasMore && (page.NextCursor == nil || strings.TrimSpace(*page.NextCursor) == "") {
		return UnitPage{}, errors.New("invalid Unit Lambung cursor page")
	}
	if page.HasMore && len(page.Items) == 0 {
		return UnitPage{}, errors.New("invalid Unit Lambung cursor page")
	}
	if !page.HasMore && page.NextCursor != nil {
		return UnitPage{}, errors.New("invalid Unit Lambung cursor page")
	}
	return page, nil
}

func (c *Client) getJSON(ctx context.Context, baseURL, token, path string, destination any) error {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return errors.New("invalid integration base URL")
	}
	relative, err := url.Parse(path)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(relative).String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.http.Do(request)
	if err != nil {
		return errors.New("call integration")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("integration returned status %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("parse integration response")
	}
	return nil
}

func parseID(raw json.RawMessage) (int64, error) {
	value := strings.Trim(string(raw), `"`)
	return strconv.ParseInt(value, 10, 64)
}
