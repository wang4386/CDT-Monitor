package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wang4386/CDT-Monitor/internal/domain"
)

type Provider interface {
	GetTraffic(ctx context.Context, account domain.Account, secret string) (float64, error)
	GetInstanceStatus(ctx context.Context, account domain.Account, secret string) (string, error)
	GetInstanceDetails(ctx context.Context, account domain.Account, secret string) (InstanceDetails, error)
	ControlInstance(ctx context.Context, account domain.Account, secret, action, shutdownMode string) error
	GetAccountBalance(ctx context.Context, account domain.Account, secret string) (BillingBalance, error)
	GetInstanceBill(ctx context.Context, account domain.Account, secret, cycle string) (BillingBill, error)
}

type InstanceDetails struct {
	Status     string
	PublicIP   string
	PublicIPv6 string
	// An optional ENI lookup failure must not discard the instance status or IPv4.
	IPv6Error error
}

type BillingBalance struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type BillingBill struct {
	TotalCost float64 `json:"total_cost"`
}

type Client struct {
	httpClient *http.Client
	trafficMu  sync.Mutex
	traffic    map[string]trafficCacheEntry
	balanceMu  sync.Mutex
	balance    map[string]balanceCacheEntry
}

type trafficCacheEntry struct {
	value     float64
	createdAt time.Time
}
type balanceCacheEntry struct {
	value     BillingBalance
	createdAt time.Time
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 18 * time.Second},
		traffic:    make(map[string]trafficCacheEntry),
		balance:    make(map[string]balanceCacheEntry),
	}
}

func (c *Client) GetTraffic(ctx context.Context, account domain.Account, secret string) (float64, error) {
	key := account.AccessKeyID + ":" + trafficClass(account.RegionID)
	c.trafficMu.Lock()
	if cached, ok := c.traffic[key]; ok && time.Since(cached.createdAt) < 45*time.Second {
		c.trafficMu.Unlock()
		return cached.value, nil
	}
	c.trafficMu.Unlock()
	result, err := c.call(ctx, account.AccessKeyID, secret, "cn-hongkong", "cdt.aliyuncs.com", "2021-08-13", "ListCdtInternetTraffic", nil)
	if err != nil {
		return 0, err
	}
	value, err := trafficFromResponse(result, trafficClass(account.RegionID))
	if err != nil {
		return 0, err
	}
	c.trafficMu.Lock()
	c.traffic[key] = trafficCacheEntry{value: value, createdAt: time.Now()}
	c.trafficMu.Unlock()
	return value, nil
}

func trafficClass(region string) string {
	if strings.HasPrefix(region, "cn-") && region != "cn-hongkong" {
		return "china"
	}
	return "international"
}

func (c *Client) GetInstanceStatus(ctx context.Context, account domain.Account, secret string) (string, error) {
	params := map[string]string{"RegionId": account.RegionID}
	if account.InstanceID != "" {
		params["InstanceId"] = account.InstanceID
	}
	result, err := c.call(ctx, account.AccessKeyID, secret, account.RegionID, "ecs."+account.RegionID+".aliyuncs.com", "2014-05-26", "DescribeInstanceStatus", params)
	if err != nil {
		return domain.StatusUnknown, err
	}
	statuses := nestedSlice(result, "InstanceStatuses", "InstanceStatus")
	if len(statuses) == 0 {
		return domain.StatusUnknown, nil
	}
	first, ok := statuses[0].(map[string]any)
	if !ok {
		return domain.StatusUnknown, nil
	}
	status, _ := first["Status"].(string)
	if status == "" {
		return domain.StatusUnknown, nil
	}
	return status, nil
}

func (c *Client) GetInstanceDetails(ctx context.Context, account domain.Account, secret string) (InstanceDetails, error) {
	if account.InstanceID == "" {
		return InstanceDetails{}, errors.New("instance_id is required")
	}
	instanceIDs, err := json.Marshal([]string{account.InstanceID})
	if err != nil {
		return InstanceDetails{}, err
	}
	params := map[string]string{
		"RegionId": account.RegionID, "InstanceIds": string(instanceIDs),
		"AdditionalAttributes.1": "NETWORK_PRIMARY_ENI_IP",
	}
	result, err := c.call(ctx, account.AccessKeyID, secret, account.RegionID, "ecs."+account.RegionID+".aliyuncs.com", "2014-05-26", "DescribeInstances", params)
	if err != nil {
		return InstanceDetails{}, err
	}
	instances := nestedSlice(result, "Instances", "Instance")
	if len(instances) == 0 {
		if container, ok := result["Instances"].(map[string]any); ok {
			if instance, ok := container["Instance"].(map[string]any); ok {
				instances = []any{instance}
			}
		}
	}
	if len(instances) == 0 {
		return InstanceDetails{}, fmt.Errorf("aliyun DescribeInstances returned no instance %q", account.InstanceID)
	}
	instance, ok := instances[0].(map[string]any)
	if !ok {
		return InstanceDetails{}, errors.New("aliyun DescribeInstances returned an invalid instance")
	}
	publicIP := firstAddress(instance["EipAddress"])
	if publicIP == "" {
		publicIP = firstAddress(instance["PublicIpAddress"])
	}
	publicIPv6 := firstAddress(instance["Ipv6Address"])
	if publicIPv6 == "" {
		publicIPv6 = firstAddress(instance["Ipv6Addresses"])
	}
	if publicIPv6 == "" {
		publicIPv6 = firstNetworkInterfaceIPv6(instance)
	}
	details := InstanceDetails{Status: stringValue(instance["Status"]), PublicIP: publicIP, PublicIPv6: publicIPv6}
	if details.PublicIPv6 == "" {
		// Bound the whole optional lookup, including retries and pagination.
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		details.PublicIPv6, details.IPv6Error = c.getNetworkInterfaceIPv6(lookupCtx, account, secret)
		if ctx.Err() != nil {
			return InstanceDetails{}, ctx.Err()
		}
	}
	return details, nil
}

func (c *Client) getNetworkInterfaceIPv6(ctx context.Context, account domain.Account, secret string) (string, error) {
	params := map[string]string{
		"RegionId": account.RegionID, "InstanceId": account.InstanceID, "MaxResults": "100",
	}
	// Omit Type so both primary and secondary ENIs are included.
	var interfaces []any
	seenTokens := make(map[string]bool)
	for {
		result, err := c.call(ctx, account.AccessKeyID, secret, account.RegionID, "ecs."+account.RegionID+".aliyuncs.com", "2014-05-26", "DescribeNetworkInterfaces", params)
		if err != nil {
			return "", err
		}
		sets, ok := result["NetworkInterfaceSets"].(map[string]any)
		if !ok {
			return "", errors.New("aliyun DescribeNetworkInterfaces missing NetworkInterfaceSets")
		}
		page, ok := sets["NetworkInterfaceSet"].([]any)
		if !ok {
			return "", errors.New("aliyun DescribeNetworkInterfaces invalid NetworkInterfaceSet")
		}
		for _, item := range page {
			nic, ok := item.(map[string]any)
			if !ok {
				return "", errors.New("aliyun DescribeNetworkInterfaces returned an invalid interface")
			}
			if id := stringValue(nic["InstanceId"]); id != "" && id != account.InstanceID {
				continue
			}
			interfaces = append(interfaces, nic)
		}
		token := stringValue(result["NextToken"])
		if token == "" {
			return firstInterfaceIPv6(interfaces), nil
		}
		if seenTokens[token] {
			return "", errors.New("aliyun DescribeNetworkInterfaces repeated NextToken")
		}
		seenTokens[token] = true
		params["NextToken"] = token
	}
}

func (c *Client) ControlInstance(ctx context.Context, account domain.Account, secret, action, shutdownMode string) error {
	if account.InstanceID == "" {
		return errors.New("instance_id is required")
	}
	params := map[string]string{"RegionId": account.RegionID, "InstanceId": account.InstanceID}
	action = strings.ToLower(action)
	apiAction := "StartInstance"
	if action == "stop" {
		apiAction = "StopInstance"
		if shutdownMode != "StopCharging" {
			shutdownMode = "KeepCharging"
		}
		params["StoppedMode"] = shutdownMode
	}
	_, err := c.call(ctx, account.AccessKeyID, secret, account.RegionID, "ecs."+account.RegionID+".aliyuncs.com", "2014-05-26", apiAction, params)
	return err
}

func (c *Client) GetAccountBalance(ctx context.Context, account domain.Account, secret string) (BillingBalance, error) {
	key := account.AccessKeyID + ":" + account.SiteType
	c.balanceMu.Lock()
	if cached, ok := c.balance[key]; ok && time.Since(cached.createdAt) < 6*time.Hour {
		c.balanceMu.Unlock()
		return cached.value, nil
	}
	c.balanceMu.Unlock()
	bss := bssEndpoint(account.SiteType)
	result, err := c.call(ctx, account.AccessKeyID, secret, bss.region, bss.host, "2017-12-14", "QueryAccountBalance", nil)
	if err != nil {
		return BillingBalance{}, err
	}
	data, _ := result["Data"].(map[string]any)
	value := BillingBalance{Amount: number(data["AvailableAmount"]), Currency: stringValue(data["Currency"])}
	if value.Currency == "" {
		value.Currency = "CNY"
	}
	c.balanceMu.Lock()
	c.balance[key] = balanceCacheEntry{value: value, createdAt: time.Now()}
	c.balanceMu.Unlock()
	return value, nil
}

func (c *Client) GetInstanceBill(ctx context.Context, account domain.Account, secret, cycle string) (BillingBill, error) {
	bss := bssEndpoint(account.SiteType)
	params := map[string]string{"BillingCycle": cycle, "InstanceID": account.InstanceID, "Granularity": "MONTHLY"}
	result, err := c.call(ctx, account.AccessKeyID, secret, bss.region, bss.host, "2017-12-14", "DescribeInstanceBill", params)
	if err != nil {
		return BillingBill{}, err
	}
	data, _ := result["Data"].(map[string]any)
	items := asSlice(data["Items"])
	if nested := nestedSlice(data, "Items", "Item"); len(nested) > 0 {
		items = nested
	}
	var total float64
	for _, item := range items {
		if obj, ok := item.(map[string]any); ok {
			total += number(obj["PretaxAmount"])
		}
	}
	return BillingBill{TotalCost: math.Round(total*100) / 100}, nil
}

type bssConfig struct{ region, host string }

func bssEndpoint(siteType string) bssConfig {
	if siteType == "international" {
		return bssConfig{region: "ap-southeast-1", host: "business.ap-southeast-1.aliyuncs.com"}
	}
	return bssConfig{region: "cn-hangzhou", host: "business.aliyuncs.com"}
}

func (c *Client) call(ctx context.Context, accessKeyID, secret, region, host, version, action string, extras map[string]string) (map[string]any, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		result, retry, err := c.callOnce(ctx, accessKeyID, secret, region, host, version, action, extras)
		if err == nil {
			return result, nil
		}
		last = err
		if !retry || attempt == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(1<<attempt)*300*time.Millisecond + time.Duration(attempt*100)*time.Millisecond):
		}
	}
	return nil, last
}

func (c *Client) callOnce(ctx context.Context, accessKeyID, secret, region, host, version, action string, extras map[string]string) (map[string]any, bool, error) {
	params := map[string]string{
		"AccessKeyId":      accessKeyID,
		"Action":           action,
		"Format":           "JSON",
		"RegionId":         region,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   randomNonce(),
		"SignatureVersion": "1.0",
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          version,
	}
	for key, value := range extras {
		params[key] = value
	}
	params["Signature"] = sign(params, secret)
	form := url.Values{}
	for key, value := range params {
		form.Set(key, value)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, true, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode >= 500, err
	}
	var result map[string]any
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, resp.StatusCode >= 500, fmt.Errorf("aliyun %s invalid response: %w", action, err)
	}
	if resp.StatusCode >= 400 {
		message := compactMessage(result, body)
		return nil, resp.StatusCode >= 500 || resp.StatusCode == 429 || isRetryableTimestampError(stringValue(result["Code"]), message), fmt.Errorf("aliyun %s http %d: %s", action, resp.StatusCode, message)
	}
	if code := stringValue(result["Code"]); code != "" && !isSuccessCode(code) {
		message := stringValue(result["Message"])
		return nil, strings.Contains(strings.ToLower(code), "throttl") || isRetryableTimestampError(code, message), fmt.Errorf("aliyun %s %s: %s", action, code, message)
	}
	if action == "ListCdtInternetTraffic" && !hasTrafficDetails(result) {
		return nil, true, errors.New("CDT response has no TrafficDetails")
	}
	return result, false, nil
}

func isSuccessCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "ok", "200", "success":
		return true
	default:
		return false
	}
}

func compactMessage(result map[string]any, raw []byte) string {
	if message := stringValue(result["Message"]); message != "" {
		return message
	}
	return string(raw)
}

func isRetryableTimestampError(code, message string) bool {
	text := strings.ToLower(strings.ReplaceAll(code+" "+message, " ", ""))
	return strings.Contains(text, "timestamp") &&
		(strings.Contains(text, "expired") || strings.Contains(text, "notsupplied") || strings.Contains(text, "missing"))
}

func sign(values map[string]string, secret string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "Signature" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for index, key := range keys {
		if index > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(percentEncode(key))
		canonical.WriteByte('=')
		canonical.WriteString(percentEncode(values[key]))
	}
	stringToSign := "POST&%2F&" + percentEncode(canonical.String())
	hash := hmac.New(sha1.New, []byte(secret+"&"))
	_, _ = hash.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}

func percentEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	return strings.ReplaceAll(encoded, "%7E", "~")
}

func randomNonce() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(raw)
}

func trafficFromResponse(result map[string]any, class string) (float64, error) {
	items := asSlice(result["TrafficDetails"])
	if len(items) == 0 {
		if data, ok := result["Data"].(map[string]any); ok {
			items = asSlice(data["TrafficDetails"])
		}
	}
	if len(items) == 0 {
		return 0, errors.New("CDT response has no TrafficDetails")
	}
	var total float64
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		region := stringValue(obj["BusinessRegionId"])
		if trafficClass(region) == class {
			total += number(obj["Traffic"])
		}
	}
	return total / (1024 * 1024 * 1024), nil
}

func hasTrafficDetails(result map[string]any) bool {
	if len(asSlice(result["TrafficDetails"])) > 0 {
		return true
	}
	data, ok := result["Data"].(map[string]any)
	return ok && len(asSlice(data["TrafficDetails"])) > 0
}

func nestedSlice(value map[string]any, parents ...string) []any {
	var current any = value
	for _, key := range parents {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[key]
	}
	return asSlice(current)
}

func firstAddress(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, item := range typed {
			if address := firstAddress(item); address != "" {
				return address
			}
		}
	case map[string]any:
		for _, key := range []string{"IpAddress", "Ipv6Address", "Address"} {
			if address := firstAddress(typed[key]); address != "" {
				return address
			}
		}
	}
	return ""
}

func firstNetworkInterfaceIPv6(instance map[string]any) string {
	paths := [][]string{{"NetworkInterface"}, {"NetworkInterfaces", "NetworkInterface"}}
	var interfaces []any
	for _, path := range paths {
		interfaces = append(interfaces, nestedSlice(instance, path...)...)
	}
	return firstInterfaceIPv6(interfaces)
}

func firstInterfaceIPv6(interfaces []any) string {
	// Prefer the primary ENI even when it appears after a secondary ENI/page.
	for _, primary := range []bool{true, false} {
		for _, item := range interfaces {
			interfaceObject, ok := item.(map[string]any)
			if !ok || (stringValue(interfaceObject["Type"]) == "Primary") != primary {
				continue
			}
			addresses := nestedSlice(interfaceObject, "Ipv6Sets", "Ipv6Set")
			for _, entry := range addresses {
				address, ok := entry.(map[string]any)
				if ok && address["Primary"] == true {
					if ip := firstAddress(address); ip != "" {
						return ip
					}
				}
			}
			if address := firstAddress(addresses); address != "" {
				return address
			}
		}
	}
	return ""
}

func asSlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	if obj, ok := value.(map[string]any); ok {
		if values, ok := obj["Item"].([]any); ok {
			return values
		}
		if item, ok := obj["Item"].(map[string]any); ok {
			return []any{item}
		}
	}
	return nil
}

func number(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case json.Number:
		result, _ := number.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(number, 64)
		return result
	default:
		return 0
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
