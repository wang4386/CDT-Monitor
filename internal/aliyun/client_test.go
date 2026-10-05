package aliyun

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wang4386/CDT-Monitor/internal/domain"
)

func TestTrafficClass(t *testing.T) {
	cases := map[string]string{
		"cn-hangzhou":    "china",
		"cn-hongkong":    "international",
		"ap-southeast-1": "international",
	}
	for region, expected := range cases {
		if actual := trafficClass(region); actual != expected {
			t.Fatalf("%s: got %s", region, actual)
		}
	}
}

func TestBssEndpoints(t *testing.T) {
	if endpoint := bssEndpoint("china"); endpoint.host != "business.aliyuncs.com" || endpoint.region != "cn-hangzhou" {
		t.Fatalf("unexpected China endpoint: %#v", endpoint)
	}
	if endpoint := bssEndpoint("international"); endpoint.host != "business.ap-southeast-1.aliyuncs.com" || endpoint.region != "ap-southeast-1" {
		t.Fatalf("unexpected international endpoint: %#v", endpoint)
	}
}

func TestTrafficResponseAggregation(t *testing.T) {
	result := map[string]any{"TrafficDetails": []any{
		map[string]any{"BusinessRegionId": "cn-hangzhou", "Traffic": float64(1024 * 1024 * 1024)},
		map[string]any{"BusinessRegionId": "cn-beijing", "Traffic": float64(2 * 1024 * 1024 * 1024)},
		map[string]any{"BusinessRegionId": "cn-hongkong", "Traffic": float64(4 * 1024 * 1024 * 1024)},
	}}
	traffic, err := trafficFromResponse(result, "china")
	if err != nil || traffic != 3 {
		t.Fatalf("traffic=%v err=%v", traffic, err)
	}
}

func TestAsSliceSupportsSingleBssItem(t *testing.T) {
	items := asSlice(map[string]any{"Item": map[string]any{"PretaxAmount": "23.456"}})
	if len(items) != 1 {
		t.Fatalf("expected one BSS item, got %#v", items)
	}
	item, ok := items[0].(map[string]any)
	if !ok || number(item["PretaxAmount"]) != 23.456 {
		t.Fatalf("unexpected BSS item: %#v", items[0])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestGetAccountBalanceAcceptsAliyunBusinessCode200(t *testing.T) {
	client := NewClient()
	client.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"Code":"200","Message":"success","Data":{"AvailableAmount":"123.45"}}`)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}

	balance, err := client.GetAccountBalance(context.Background(), domain.Account{AccessKeyID: "LTAItest", SiteType: "china"}, "secret")
	if err != nil || balance.Amount != 123.45 || balance.Currency != "CNY" {
		t.Fatalf("balance=%#v err=%v", balance, err)
	}
}

func TestGetTrafficRetriesEmptyTrafficDetails(t *testing.T) {
	calls := 0
	client := NewClient()
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		body := `{"TrafficDetails":[]}`
		if calls == 2 {
			body = `{"TrafficDetails":[{"BusinessRegionId":"cn-hongkong","Traffic":1073741824}]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})

	traffic, err := client.GetTraffic(context.Background(), domain.Account{AccessKeyID: "LTAItest", RegionID: "cn-hongkong"}, "secret")
	if err != nil || traffic != 1 || calls != 2 {
		t.Fatalf("traffic=%v err=%v calls=%d", traffic, err, calls)
	}
}

func TestGetInstanceStatusRetriesTransientTimestampErrors(t *testing.T) {
	for _, message := range []string{
		`{"Code":"InvalidTimeStamp.Expired","Message":"Specified time stamp or date value is expired."}`,
		`{"Code":"MissingParameter","Message":"The input parameter Timestamp is not supplied."}`,
	} {
		t.Run(message, func(t *testing.T) {
			calls := 0
			client := NewClient()
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if err := request.ParseForm(); err != nil || request.PostForm.Get("Timestamp") == "" {
					t.Fatalf("Timestamp missing from request: %v", err)
				}
				body := message
				status := http.StatusBadRequest
				if calls == 2 {
					body = `{"InstanceStatuses":{"InstanceStatus":[{"Status":"Running"}]}}`
					status = http.StatusOK
				}
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
					Request:    request,
				}, nil
			})

			status, err := client.GetInstanceStatus(context.Background(), domain.Account{AccessKeyID: "LTAItest", RegionID: "cn-shanghai"}, "secret")
			if err != nil || status != "Running" || calls != 2 {
				t.Fatalf("status=%q err=%v calls=%d", status, err, calls)
			}
		})
	}
}
