package aliyun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestGetInstanceDetailsPrefersEIPAndParsesInstanceIDs(t *testing.T) {
	client := NewClient()
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.PostForm.Get("Action") != "DescribeInstances" || request.PostForm.Get("InstanceIds") != `["i-test"]` || request.PostForm.Get("RegionId") != "cn-hongkong" || request.PostForm.Get("AdditionalAttributes.1") != "NETWORK_PRIMARY_ENI_IP" {
			t.Fatalf("unexpected request parameters: %v", request.PostForm)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"Instances":{"Instance":[{"Status":"Running","EipAddress":{"IpAddress":"198.51.100.3"},"PublicIpAddress":{"IpAddress":["203.0.113.4"]},"Ipv6Address":{"Ipv6Address":["2001:db8::3"]}}]}}`)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})

	details, err := client.GetInstanceDetails(context.Background(), domain.Account{AccessKeyID: "LTAItest", RegionID: "cn-hongkong", InstanceID: "i-test"}, "secret")
	if err != nil || details.Status != "Running" || details.PublicIP != "198.51.100.3" || details.PublicIPv6 != "2001:db8::3" {
		t.Fatalf("details=%#v err=%v", details, err)
	}
}

func TestGetInstanceDetailsFallsBackToAssignedPublicIP(t *testing.T) {
	client := NewClient()
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		body := `{"Instances":{"Instance":{"Status":"Stopped","PublicIpAddress":{"IpAddress":["203.0.113.9"]}}}}`
		if request.PostForm.Get("Action") == "DescribeNetworkInterfaces" {
			body = `{"NetworkInterfaceSets":{"NetworkInterfaceSet":[]}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})

	details, err := client.GetInstanceDetails(context.Background(), domain.Account{AccessKeyID: "LTAItest", RegionID: "cn-hongkong", InstanceID: "i-test"}, "secret")
	if err != nil || details.IPv6Error != nil || details.Status != "Stopped" || details.PublicIP != "203.0.113.9" {
		t.Fatalf("details=%#v err=%v", details, err)
	}
}

func TestGetInstanceDetailsOmitsMissingIPv6(t *testing.T) {
	client := NewClient()
	calls := 0
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		body := `{"Instances":{"Instance":{"Status":"Running","PublicIpAddress":{"IpAddress":"203.0.113.12"}}}}`
		if request.PostForm.Get("Action") == "DescribeNetworkInterfaces" {
			body = `{"NetworkInterfaceSets":{"NetworkInterfaceSet":[]}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})

	details, err := client.GetInstanceDetails(context.Background(), domain.Account{AccessKeyID: "LTAItest", RegionID: "cn-hongkong", InstanceID: "i-test"}, "secret")
	if err != nil || details.IPv6Error != nil || details.PublicIP != "203.0.113.12" || details.PublicIPv6 != "" || calls != 2 {
		t.Fatalf("details=%#v err=%v", details, err)
	}
}

func TestGetInstanceDetailsQueriesAttachedENIs(t *testing.T) {
	cases := []struct {
		name  string
		pages []string
		want  string
	}{
		{
			name: "secondary ENI on next page",
			pages: []string{
				`{"NetworkInterfaceSets":{"NetworkInterfaceSet":[{"InstanceId":"i-other","Type":"Primary","Ipv6Sets":{"Ipv6Set":[{"Ipv6Address":"2001:db8::bad"}]}}]},"NextToken":"page2"}`,
				`{"NetworkInterfaceSets":{"NetworkInterfaceSet":[{"InstanceId":"i-test","Type":"Secondary","Ipv6Sets":{"Ipv6Set":[{"Ipv6Address":"2001:db8::2"}]}}]}}`,
			},
			want: "2001:db8::2",
		},
		{
			name: "primary ENI and primary IPv6 take precedence across pages",
			pages: []string{
				`{"NetworkInterfaceSets":{"NetworkInterfaceSet":[{"Type":"Secondary","Ipv6Sets":{"Ipv6Set":[{"Ipv6Address":"2001:db8::2"}]}}]},"NextToken":"page2"}`,
				`{"NetworkInterfaceSets":{"NetworkInterfaceSet":[{"Type":"Primary","Ipv6Sets":{"Ipv6Set":[{"Ipv6Address":"2001:db8::3"},{"Ipv6Address":"2001:db8::4","Primary":true}]}}]}}`,
			},
			want: "2001:db8::4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient()
			calls, page := 0, 0
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if err := request.ParseForm(); err != nil {
					t.Fatal(err)
				}
				body := `{"Instances":{"Instance":[{"Status":"Running","PublicIpAddress":{"IpAddress":["203.0.113.5"]}}]}}`
				if calls == 1 {
					if request.PostForm.Get("Action") != "DescribeInstances" || request.PostForm.Get("AdditionalAttributes.1") != "NETWORK_PRIMARY_ENI_IP" {
						t.Fatalf("unexpected instance query: %v", request.PostForm)
					}
				} else {
					form := request.PostForm
					if form.Get("Action") != "DescribeNetworkInterfaces" || form.Get("InstanceId") != "i-test" || form.Get("RegionId") != "cn-hongkong" || form.Get("MaxResults") != "100" || form.Has("Type") {
						t.Fatalf("unexpected ENI query: %v", form)
					}
					if (page == 0 && form.Has("NextToken")) || (page > 0 && form.Get("NextToken") != "page2") {
						t.Fatalf("unexpected pagination: %v", form)
					}
					if page >= len(tc.pages) {
						t.Fatal("too many ENI requests")
					}
					body = tc.pages[page]
					page++
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
			})
			details, err := client.GetInstanceDetails(context.Background(), domain.Account{AccessKeyID: "LTAItest", RegionID: "cn-hongkong", InstanceID: "i-test"}, "secret")
			if err != nil || details.IPv6Error != nil || details.PublicIPv6 != tc.want || details.Status != "Running" || details.PublicIP != "203.0.113.5" || page != len(tc.pages) {
				t.Fatalf("details=%#v err=%v calls=%d", details, err, calls)
			}
		})
	}
}

func TestGetInstanceDetailsENIFailurePreservesStatusAndIPv4(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"permission denied", http.StatusForbidden, `{"Code":"Forbidden","Message":"not authorized"}`, "http 403"},
		{"missing wrapper", http.StatusOK, `{}`, "NetworkInterfaceSets"},
		{"invalid list", http.StatusOK, `{"NetworkInterfaceSets":{"NetworkInterfaceSet":null}}`, "NetworkInterfaceSet"},
		{"repeated page", http.StatusOK, `{"NetworkInterfaceSets":{"NetworkInterfaceSet":[]},"NextToken":"same"}`, "repeated NextToken"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient()
			calls := 0
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				status, body := tc.status, tc.body
				if calls == 1 {
					status, body = http.StatusOK, `{"Instances":{"Instance":[{"Status":"Running","PublicIpAddress":{"IpAddress":"203.0.113.5"}}]}}`
				}
				if calls > 3 {
					t.Fatal("unbounded ENI retries")
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
			})
			details, err := client.GetInstanceDetails(context.Background(), domain.Account{RegionID: "cn-hongkong", InstanceID: "i-test"}, "secret")
			if err != nil || details.IPv6Error == nil || !strings.Contains(details.IPv6Error.Error(), tc.want) || details.Status != "Running" || details.PublicIP != "203.0.113.5" {
				t.Fatalf("details=%#v err=%v", details, err)
			}
			if tc.status == http.StatusForbidden && calls != 2 {
				t.Fatalf("permission error retried: %d calls", calls)
			}
		})
	}
}

func TestGetInstanceDetailsENITimeoutAndCancellation(t *testing.T) {
	for _, parentCancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(parentCancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := NewClient()
			calls := 0
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Instances":{"Instance":[{"Status":"Stopped","PublicIpAddress":{"IpAddress":"203.0.113.5"}}]}}`)), Header: make(http.Header), Request: request}, nil
				}
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > 3*time.Second {
					t.Fatal("ENI query has no bounded deadline")
				}
				if parentCancelled {
					cancel()
				}
				<-request.Context().Done()
				return nil, request.Context().Err()
			})
			details, err := client.GetInstanceDetails(ctx, domain.Account{RegionID: "cn-hongkong", InstanceID: "i-test"}, "secret")
			if parentCancelled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected caller cancellation, got %v", err)
				}
			} else if err != nil || !errors.Is(details.IPv6Error, context.DeadlineExceeded) || details.Status != "Stopped" || details.PublicIP != "203.0.113.5" {
				t.Fatalf("details=%#v err=%v", details, err)
			}
			if calls != 2 {
				t.Fatalf("unexpected requests after cancellation: %d", calls)
			}
		})
	}
}

func TestGetInstanceDetailsParsesIPv6FromNetworkInterfaceSets(t *testing.T) {
	client := NewClient()
	calls := 0
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.PostForm.Get("Action") != "DescribeInstances" || request.PostForm.Get("AdditionalAttributes.1") != "NETWORK_PRIMARY_ENI_IP" {
			t.Fatalf("unexpected instance request: %v", request.PostForm)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"Instances":{"Instance":[{"Status":"Running","PublicIpAddress":{"IpAddress":["203.0.113.13"]},"NetworkInterfaces":{"NetworkInterface":[{"Type":"Primary","Ipv6Sets":{"Ipv6Set":[{"Ipv6Address":"2001:db8::13","Primary":true}]}}]}}]}}`)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})

	details, err := client.GetInstanceDetails(context.Background(), domain.Account{AccessKeyID: "LTAItest", RegionID: "cn-hongkong", InstanceID: "i-test"}, "secret")
	if err != nil || details.IPv6Error != nil || details.PublicIP != "203.0.113.13" || details.PublicIPv6 != "2001:db8::13" || calls != 1 {
		t.Fatalf("details=%#v err=%v", details, err)
	}
}
