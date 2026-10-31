package clients

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"advancedmd-token-management/internal/safeerrors"
)

const (
	productInsuranceReadPath    = "/v1/agent/insurance/read"
	productInsuranceReadTimeout = 10 * time.Second
	maxProductInsuranceBytes    = 4 << 20
)

type ProductClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func NewProductClient(httpClient *http.Client, baseURL, token string) *ProductClient {
	return &ProductClient{httpClient: httpClient, baseURL: baseURL, token: token}
}

func (c *ProductClient) ReadInsuranceCatalog(ctx context.Context) (string, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, productInsuranceReadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+productInsuranceReadPath, strings.NewReader("{}"))
	if err != nil {
		return "", nil, safeerrors.New(safeerrors.CategoryInternal, "POST %s: request not built", productInsuranceReadPath)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		category := safeerrors.Classify(err)
		return "", nil, safeerrors.New(category, "POST %s: %s", productInsuranceReadPath, category)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, safeerrors.New(safeerrors.CategoryUpstreamStatus, "POST %s: HTTP %d", productInsuranceReadPath, response.StatusCode)
	}
	var revision struct {
		RevisionID string          `json:"revisionId"`
		Catalog    json.RawMessage `json:"catalog"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxProductInsuranceBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&revision); err != nil {
		return "", nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "POST %s: response is not an insurance revision: %v", productInsuranceReadPath, err)
	}
	if revision.RevisionID == "" || len(revision.Catalog) == 0 {
		return "", nil, safeerrors.New(safeerrors.CategoryInvalidResponse, "POST %s: response has no revisionId or catalog", productInsuranceReadPath)
	}
	return revision.RevisionID, revision.Catalog, nil
}
