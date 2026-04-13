package ebay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	tokenURL  = "https://api.ebay.com/identity/v1/oauth2/token"
	searchURL = "https://api.ebay.com/buy/browse/v1/item_summary/search"
)

type Client struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	token        string
	tokenExpiry  time.Time
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

type Listing struct {
	ID        string
	Title     string
	Price     float64
	Currency  string
	URL       string
	Condition string
	Seller    string
}

type SearchResult struct {
	Listings []Listing
	Total    int
}

type CategorySuggestion struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MatchCount int    `json:"match_count"`
}

func (c *Client) Search(ctx context.Context, query string, maxPrice float64, categoryID string) (*SearchResult, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("filter", fmt.Sprintf(
		"price:[..%s],priceCurrency:USD,conditions:{USED|NEW|SELLER_REFURBISHED|EXCELLENT_REFURBISHED|VERY_GOOD_REFURBISHED|GOOD_REFURBISHED|LIKE_NEW}",
		strconv.FormatFloat(maxPrice, 'f', 2, 64),
	))
	if categoryID != "" {
		params.Set("category_ids", categoryID)
	}
	params.Set("sort", "price")
	params.Set("limit", "50")

	reqURL := searchURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", "EBAY_US")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ebay API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Total        int `json:"total"`
		ItemSummaries []struct {
			ItemID string `json:"itemId"`
			Title  string `json:"title"`
			Price  struct {
				Value    string `json:"value"`
				Currency string `json:"currency"`
			} `json:"price"`
			ItemWebURL string `json:"itemWebUrl"`
			Condition  string `json:"condition"`
			Seller     struct {
				Username string `json:"username"`
			} `json:"seller"`
		} `json:"itemSummaries"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := &SearchResult{Total: apiResp.Total}
	for _, item := range apiResp.ItemSummaries {
		price, err := strconv.ParseFloat(item.Price.Value, 64)
		if err != nil {
			slog.Warn("could not parse price", "item_id", item.ItemID, "price", item.Price.Value)
			continue
		}
		result.Listings = append(result.Listings, Listing{
			ID:        item.ItemID,
			Title:     item.Title,
			Price:     price,
			Currency:  item.Price.Currency,
			URL:       item.ItemWebURL,
			Condition: item.Condition,
			Seller:    item.Seller.Username,
		})
	}

	return result, nil
}

func (c *Client) SearchCategories(ctx context.Context, query string) ([]CategorySuggestion, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("fieldgroups", "CATEGORY_REFINEMENTS")
	params.Set("limit", "1")

	reqURL := searchURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", "EBAY_US")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("category request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ebay API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Refinement struct {
			CategoryDistributions []struct {
				CategoryID   string `json:"categoryId"`
				CategoryName string `json:"categoryName"`
				MatchCount   int    `json:"matchCount"`
			} `json:"categoryDistributions"`
		} `json:"refinement"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var suggestions []CategorySuggestion
	for _, cat := range apiResp.Refinement.CategoryDistributions {
		suggestions = append(suggestions, CategorySuggestion{
			ID:         cat.CategoryID,
			Name:       cat.CategoryName,
			MatchCount: cat.MatchCount,
		})
	}
	return suggestions, nil
}

func (c *Client) ensureToken(ctx context.Context) error {
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return nil
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("scope", "https://api.ebay.com/oauth/api_scope")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token error %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}

	c.token = tokenResp.AccessToken
	// Expire 60s early to avoid races
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	slog.Info("ebay token refreshed", "expires_in", tokenResp.ExpiresIn)
	return nil
}
