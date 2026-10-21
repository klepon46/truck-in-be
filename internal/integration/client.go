package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	number = strings.TrimSpace(number)
	var response struct {
		Success *bool  `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			Number     string `json:"work_order_number"`
			DriverName string `json:"driver_name"`
			VehicleNo  string `json:"vehicle_no"`
			PoliceNo   string `json:"police_no"`
		} `json:"data"`
	}
	status, err := c.getJSONStatus(ctx, c.config.WorkshopBaseURL, c.config.WorkshopToken, "/internal/v1/work-orders/by-wo?wo-number="+url.QueryEscape(number), &response, true)
	if err != nil {
		slog.ErrorContext(ctx, "WO upstream lookup failed", "error", err)
		return movement.Document{}, fmt.Errorf("%w: Workshop lookup: %w", movement.ErrIntegrationUnavailable, err)
	}
	if status == http.StatusNotFound {
		// Match Workshop's explicit absence contract, never a generic route/proxy 404.
		if response.Success != nil && !*response.Success && response.Code == http.StatusNotFound && response.Message == "Work order not found" && response.Data == nil {
			return movement.Document{}, movement.ErrWorkOrderNotFound
		}
		return movement.Document{}, fmt.Errorf("%w: Workshop returned an unexpected 404 response", movement.ErrIntegrationUnavailable)
	}
	if response.Success == nil || (*response.Success && response.Data == nil) {
		return movement.Document{}, fmt.Errorf("%w: Workshop returned an invalid response envelope", movement.ErrIntegrationUnavailable)
	}
	if !*response.Success || response.Data.Number != number {
		return movement.Document{}, movement.ErrExternalDocument
	}
	document := movement.Document{
		NoLambung:  strings.ToUpper(strings.TrimSpace(response.Data.VehicleNo)),
		NoPolisi:   strings.ToUpper(strings.TrimSpace(response.Data.PoliceNo)),
		DriverName: strings.TrimSpace(response.Data.DriverName),
	}
	if document.NoLambung == "" || document.NoPolisi == "" || document.DriverName == "" {
		return movement.Document{}, movement.ErrExternalDocument
	}
	return document, nil
}

func (c *Client) SPP(ctx context.Context, number string) (movement.Document, error) {
	number = strings.TrimSpace(number)
	var response struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := c.getJSON(ctx, c.config.SAOSBaseURL, c.config.SAOSToken, "/api/v2/loading-orders?search="+url.QueryEscape(number), &response); err != nil {
		// getJSON returns only sanitized failure reasons, never a URL, token, or body.
		slog.ErrorContext(ctx, "SPP upstream lookup failed", "error", err)
		return movement.Document{}, fmt.Errorf("%w: SAOS lookup: %w", movement.ErrIntegrationUnavailable, err)
	}
	if response.Success == nil {
		slog.ErrorContext(ctx, "SPP upstream lookup failed", "reason", "invalid_response_envelope")
		return movement.Document{}, fmt.Errorf("%w: SAOS returned an invalid response envelope", movement.ErrIntegrationUnavailable)
	}
	if !*response.Success {
		slog.WarnContext(ctx, "SPP document rejected", "reason", "unsuccessful_response")
		return movement.Document{}, movement.ErrExternalDocument
	}
	if len(response.Data) == 0 {
		slog.ErrorContext(ctx, "SPP upstream lookup failed", "reason", "missing_data_array")
		return movement.Document{}, fmt.Errorf("%w: SAOS response is missing its data array", movement.ErrIntegrationUnavailable)
	}
	var records []struct {
		Number       string `json:"trans_no"`
		DriverName   string `json:"driver_name"`
		CustomerID   string `json:"customer_code"`
		CustomerName string `json:"customer_name"`
		VehicleNo    string `json:"unit_name"`
		PoliceNo     string `json:"unit_police_no"`
	}
	// SAOS returns a nil slice as explicit null when a successful query has no rows.
	if err := json.Unmarshal(response.Data, &records); err != nil {
		return movement.Document{}, fmt.Errorf("%w: SAOS returned an invalid data array", movement.ErrIntegrationUnavailable)
	}
	for _, record := range records {
		if record.Number == number {
			document := movement.Document{
				NoLambung:    strings.ToUpper(strings.TrimSpace(record.VehicleNo)),
				NoPolisi:     strings.ToUpper(strings.TrimSpace(record.PoliceNo)),
				DriverName:   strings.TrimSpace(record.DriverName),
				CustomerID:   strings.TrimSpace(record.CustomerID),
				CustomerName: strings.TrimSpace(record.CustomerName),
			}
			if document.NoLambung == "" || document.NoPolisi == "" || document.DriverName == "" {
				slog.WarnContext(ctx, "SPP document rejected", "reason", "missing_required_fields",
					"missing_unit", document.NoLambung == "", "missing_police", document.NoPolisi == "", "missing_driver", document.DriverName == "")
				return movement.Document{}, movement.ErrExternalDocument
			}
			return document, nil
		}
	}
	slog.WarnContext(ctx, "SPP document rejected", "reason", "no_matching_trans_no")
	return movement.Document{}, movement.ErrSPPNotFound
}

type ActiveDriver struct {
	ID         int64  `json:"id"`
	DriverName string `json:"driverName"`
}

func (c *Client) ActiveDrivers(ctx context.Context, name string) ([]ActiveDriver, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("driver name is required")
	}

	var response struct {
		Success *bool `json:"success"`
		Data    *[]struct {
			ID         json.RawMessage `json:"id"`
			DriverName string          `json:"driverName"`
			ResignDate *string         `json:"resignDate"`
		} `json:"data"`
	}
	path := "/internal/v1/active-drivers?name=" + url.QueryEscape(name)
	if err := c.getJSON(ctx, c.config.DriverBaseURL, c.config.DriverToken, path, &response); err != nil {
		return nil, err
	}
	if response.Success == nil || !*response.Success || response.Data == nil {
		return nil, errors.New("invalid active drivers response")
	}

	drivers := make([]ActiveDriver, 0, len(*response.Data))
	for _, driver := range *response.Data {
		id, err := parseID(driver.ID)
		name := strings.TrimSpace(driver.DriverName)
		if err != nil || id < 1 || name == "" {
			return nil, errors.New("invalid active drivers response")
		}
		if driver.ResignDate == nil || strings.TrimSpace(*driver.ResignDate) == "" {
			drivers = append(drivers, ActiveDriver{ID: id, DriverName: name})
		}
	}
	return drivers, nil
}

func (c *Client) Driver(ctx context.Context, id int64, name string) error {
	drivers, err := c.ActiveDrivers(ctx, name)
	if err != nil {
		return err
	}
	for _, driver := range drivers {
		if driver.ID == id && strings.EqualFold(driver.DriverName, strings.TrimSpace(name)) {
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
	_, err := c.getJSONStatus(ctx, baseURL, token, path, destination, false)
	return err
}

// Only Workshop opts into decoding 404 to verify its documented not-found envelope.
func (c *Client) getJSONStatus(ctx context.Context, baseURL, token, path string, destination any, decodeNotFound bool) (int, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return 0, errors.New("invalid integration base URL")
	}
	relative, err := url.Parse(path)
	if err != nil {
		return 0, errors.New("invalid integration path")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(relative).String(), nil)
	if err != nil {
		return 0, errors.New("create integration request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return 0, errors.New("call integration")
	}
	defer response.Body.Close()
	if (response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices) && !(decodeNotFound && response.StatusCode == http.StatusNotFound) {
		return response.StatusCode, fmt.Errorf("integration returned status %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(destination); err != nil {
		return response.StatusCode, errors.New("parse integration response")
	}
	return response.StatusCode, nil
}

func parseID(raw json.RawMessage) (int64, error) {
	value := strings.Trim(string(raw), `"`)
	return strconv.ParseInt(value, 10, 64)
}
