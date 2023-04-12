package instantly

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/ratelimit"
)

type Option func(option *options) error

type options struct {
	host       string
	apiVersion int
	rateLimit  *ratelimit.Limiter
	httpClient *http.Client
}

func WithHost(host string) Option {
	return func(option *options) error {
		// Check if host is valid.
		_, err := http.NewRequest("GET", fmt.Sprintf("https://%s", host), nil)
		if err != nil {
			return errors.New("invalid host: " + err.Error())
		}

		option.host = host
		return nil
	}
}

func WithApiVersion(version int) Option {
	return func(option *options) error {
		if version < 1 {
			return errors.New("invalid api version: " + strconv.Itoa(version))
		}

		option.apiVersion = version
		return nil
	}
}

func WithRateLimit(rl ratelimit.Limiter) Option {
	return func(option *options) error {
		option.rateLimit = &rl
		return nil
	}
}

func WithHttpClient(hc http.Client) Option {
	return func(option *options) error {
		option.httpClient = &hc
		return nil
	}
}

type Client struct {
	apiKey  string
	options *options
}

func New(apiKey string, opts ...Option) (*Client, error) {
	o := &options{}
	for _, opt := range opts {
		err := opt(o)
		if err != nil {
			return nil, err
		}
	}

	// Set default values.
	if o.host == "" {
		o.host = "api.instantly.ai"
	}
	if o.apiVersion == 0 {
		o.apiVersion = 1
	}
	if o.rateLimit == nil {
		// Our platform allows a maximum of 10 requests per second to prevent abuse.
		// https://developer.instantly.ai/introduction/rate_limits
		o.rateLimit = new(ratelimit.Limiter)
		*o.rateLimit = ratelimit.New(10, ratelimit.Per(time.Second))
	}
	if o.httpClient == nil {
		o.httpClient = http.DefaultClient
	}

	return &Client{apiKey: apiKey, options: o}, nil
}

type query struct {
	key   string
	value string
}

func param(key, value string) query {
	return query{
		key:   key,
		value: value,
	}
}

func (c *Client) buildBodyUrl(path string) string {
	return fmt.Sprintf("https://%s/api/v%d/%s", c.options.host, c.options.apiVersion, path)
}

func (c *Client) buildQueryUrl(path string, params []query) string {
	url := c.buildBodyUrl(path)
	url = fmt.Sprintf("%s?api_key=%s", url, c.apiKey)
	for _, param := range params {
		url = fmt.Sprintf("%s&%s=%s", url, param.key, param.value)
	}

	return url
}

func (c *Client) call(method, url string, body io.Reader) (data []byte, err error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, errors.New("failed to create request: " + err.Error())
	}

	// If body is not nil, set content type to json.
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Wait for rate limit.
	(*c.options.rateLimit).Take()
	res, err := c.options.httpClient.Do(req)
	if err != nil {
		return nil, errors.New("failed to execute request: " + err.Error())
	}
	defer res.Body.Close()

	data, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, errors.New("failed to read response body: " + err.Error())
	}

	return data, nil
}

func (c *Client) get(path string, params []query) (data []byte, err error) {
	url := c.buildQueryUrl(path, params)
	data, err = c.call("GET", url, nil)
	if err != nil {
		return nil, errors.New("failed to execute request: " + err.Error())
	}

	return data, nil
}

func (c *Client) post(path string, body any) (data []byte, err error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, errors.New("failed to marshal body: " + err.Error())
	}

	var bodyMap map[string]interface{}
	err = json.Unmarshal(jsonBody, &bodyMap)
	if err != nil {
		return nil, errors.New("failed to unmarshal body: " + err.Error())
	}

	bodyMap["api_key"] = c.apiKey

	jsonBody, err = json.Marshal(bodyMap)
	if err != nil {
		return nil, errors.New("failed to marshal body: " + err.Error())
	}

	url := c.buildBodyUrl(path)
	data, err = c.call("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, errors.New("failed to execute request: " + err.Error())
	}

	return data, nil
}

func (c *Client) Authenticate() (workspaceName string, err error) {
	data, err := c.get("authenticate", nil)
	if err != nil {
		return "", errors.New("failed to authenticate: " + err.Error())
	}

	return string(data), nil
}

type Campaign struct {
	Id   string
	Name string
}

type listCampaignsResponse []struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListCampaigns() ([]Campaign, error) {
	data, err := c.get("campaign/list", nil)
	if err != nil {
		return nil, errors.New("failed to list campaigns: " + err.Error())
	}

	res := &listCampaignsResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return nil, errors.New("failed to unmarshal campaigns: " + err.Error())
	}

	var campaigns []Campaign
	for _, campaign := range *res {
		campaigns = append(campaigns, Campaign{
			Id:   campaign.Id,
			Name: campaign.Name,
		})
	}

	return campaigns, nil
}

type getCampaignNameResponse struct {
	Id   string `json:"campaign_id"`
	Name string `json:"campaign_name"`
}

func (c *Client) GetCampaignName(campaignId string) (campaignName string, err error) {
	data, err := c.get("campaign/get/name", []query{param("campaign_id", campaignId)})
	if err != nil {
		return "", errors.New("failed to get campaign name: " + err.Error())
	}

	res := &getCampaignNameResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return "", errors.New("failed to unmarshal campaign name: " + err.Error())
	}

	return res.Name, nil
}

type setCampaignNamePayload struct {
	CampaignId string `json:"campaign_id"`
	Name       string `json:"name"`
}

type setCampaignNameResponse struct {
	Status string `json:"status"`
}

func (c *Client) SetCampaignName(campaignId, campaignName string) error {
	payload := setCampaignNamePayload{
		CampaignId: campaignId,
		Name:       campaignName,
	}

	data, err := c.post("campaign/set/name", payload)
	if err != nil {
		return errors.New("failed to set campaign name: " + err.Error())
	}

	res := &setCampaignNameResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return errors.New("failed to unmarshal campaign name: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to set campaign name: " + res.Status)
	}

	return nil
}

func (c *Client) GetCampaignAccounts(campaignId string) (accountEmails []string, err error) {
	data, err := c.get("campaign/get/accounts", []query{param("campaign_id", campaignId)})
	if err != nil {
		return nil, errors.New("failed to get campaign accounts: " + err.Error())
	}

	var res []string
	err = json.Unmarshal(data, &res)
	if err != nil {
		return nil, errors.New("failed to unmarshal campaign accounts: " + err.Error())
	}

	return res, nil
}

type setCampaignAccountsPayload struct {
	CampaignId  string   `json:"campaign_id"`
	AccountList []string `json:"account_list"`
}

type setCampaignAccountsResponse struct {
	Status string `json:"status"`
}

func (c *Client) SetCampaignAccounts(campaignId string, accountEmails []string) error {
	payload := setCampaignAccountsPayload{
		CampaignId:  campaignId,
		AccountList: accountEmails,
	}

	data, err := c.post("campaign/set/accounts", payload)
	if err != nil {
		return errors.New("failed to set campaign accounts: " + err.Error())
	}

	res := &setCampaignAccountsResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return errors.New("failed to unmarshal campaign accounts: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to set campaign accounts: " + res.Status)
	}

	return nil
}

type addSendingAccountPayload struct {
	CampaignId string `json:"campaign_id"`
	Email      string `json:"email"`
}

type addSendingAccountResponse struct {
	Status string `json:"status"`
}

func (c *Client) AddSendingAccount(campaignId, email string) error {
	payload := addSendingAccountPayload{
		CampaignId: campaignId,
		Email:      email,
	}

	data, err := c.post("campaign/add/account", payload)
	if err != nil {
		return errors.New("failed to add sending account: " + err.Error())
	}

	res := &addSendingAccountResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return errors.New("failed to unmarshal sending account: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to add sending account: " + res.Status)
	}

	return nil
}

type removeSendingAccountPayload struct {
	CampaignId string `json:"campaign_id"`
	Email      string `json:"email"`
}

type removeSendingAccountResponse struct {
	Status string `json:"status"`
}

func (c *Client) RemoveSendingAccount(campaignId, email string) error {
	payload := removeSendingAccountPayload{
		CampaignId: campaignId,
		Email:      email,
	}

	data, err := c.post("campaign/remove/account", payload)
	if err != nil {
		return errors.New("failed to remove sending account: " + err.Error())
	}

	res := &removeSendingAccountResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return errors.New("failed to unmarshal sending account: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to remove sending account: " + res.Status)
	}

	return nil
}

type internalSetCampaignSchedulePayload struct {
	CampaignId string     `json:"campaign_id"`
	StartDate  time.Time  `json:"start_date"`
	EndDate    *time.Time `json:"end_date,omitempty"`
	Schedules  []CampaignSchedule
}

type CampaignSchedule struct {
	Name     string
	Days     map[time.Weekday]bool
	Timezone *time.Location
	Timing   Timing
}

type Timing struct {
	From time.Time
	To   time.Time
}

type setCampaignSchedulePayload struct {
	CampaignId string             `json:"campaign_id"`
	StartDate  string             `json:"start_date"`
	EndDate    string             `json:"end_date,omitempty"`
	Schedules  []campaignSchedule `json:"schedules"`
}

type campaignSchedule struct {
	Name     string          `json:"name"`
	Days     map[string]bool `json:"days"`
	Timezone string          `json:"timezone"`
	Timing   timing          `json:"timing"`
}

type timing struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (p *internalSetCampaignSchedulePayload) convert() (*setCampaignSchedulePayload, error) {
	payload := &setCampaignSchedulePayload{
		CampaignId: p.CampaignId,
		StartDate:  p.StartDate.Format("2006-01-02"),
		Schedules:  make([]campaignSchedule, len(p.Schedules)),
	}

	if p.EndDate != nil {
		payload.EndDate = p.EndDate.Format("2006-01-02")
	}

	for i, goNativeSchedule := range p.Schedules {
		schedule := campaignSchedule{
			Name:     goNativeSchedule.Name,
			Days:     make(map[string]bool),
			Timezone: goNativeSchedule.Timezone.String(),
		}

		// Convert days
		for day, value := range goNativeSchedule.Days {
			schedule.Days[strconv.Itoa(int(day))] = value
		}

		// Convert timing
		schedule.Timing.From = goNativeSchedule.Timing.From.Format("15:04")
		schedule.Timing.To = goNativeSchedule.Timing.To.Format("15:04")

		payload.Schedules[i] = schedule
	}

	return payload, nil
}

type setCampaignScheduleResponse struct {
	Status string `json:"status"`
}

func (c *Client) SetCampaignSchedule(campaignId string, startDate time.Time, endDate *time.Time, schedules []CampaignSchedule) error {
	internalPayload := &internalSetCampaignSchedulePayload{
		CampaignId: campaignId,
		StartDate:  startDate,
		EndDate:    endDate,
		Schedules:  schedules,
	}

	payload, err := internalPayload.convert()
	if err != nil {
		return errors.New("failed to convert campaign schedule: " + err.Error())
	}

	data, err := c.post("campaign/set/schedules", payload)
	if err != nil {
		return errors.New("failed to set campaign schedule: " + err.Error())
	}

	res := &setCampaignScheduleResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return errors.New("failed to unmarshal campaign schedule: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to set campaign schedule: " + res.Status)
	}

	return nil
}

type launchCampaignPayload struct {
	CampaignId string `json:"campaign_id"`
}

type launchCampaignResponse struct {
	Status string `json:"status"`
}

func (c *Client) LaunchCampaign(campaignId string) error {
	payload := launchCampaignPayload{
		CampaignId: campaignId,
	}

	data, err := c.post("campaign/launch", payload)
	if err != nil {
		return errors.New("failed to launch campaign: " + err.Error())
	}

	res := &launchCampaignResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return errors.New("failed to unmarshal campaign launch: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to launch campaign: " + res.Status)
	}

	return nil
}

type pauseCampaignPayload struct {
	CampaignId string `json:"campaign_id"`
}

type pauseCampaignResponse struct {
	Status string `json:"status"`
}

func (c *Client) PauseCampaign(campaignId string) error {
	payload := pauseCampaignPayload{
		CampaignId: campaignId,
	}

	data, err := c.post("campaign/pause", payload)
	if err != nil {
		return errors.New("failed to pause campaign: " + err.Error())
	}

	res := &pauseCampaignResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return errors.New("failed to unmarshal campaign pause: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to pause campaign: " + res.Status)
	}

	return nil
}

type getCampaignSummaryResponse struct {
	CampaignID      string `json:"campaign_id"`
	CampaignName    string `json:"campaign_name"`
	TotalLeads      int    `json:"total_leads"`
	Contacted       int    `json:"contacted"`
	LeadsWhoRead    int    `json:"leads_who_read"`
	LeadsWhoReplied int    `json:"leads_who_replied"`
	Bounced         string `json:"bounced"`
	Unsubscribed    string `json:"unsubscribed"`
	Completed       int    `json:"completed"`
}

func (c *Client) GetCampaignSummary(campaignId string) (summary *getCampaignSummaryResponse, err error) {
	data, err := c.get("campaign/summary", []query{param("campaign_id", campaignId)})
	if err != nil {
		return nil, errors.New("failed to get campaign summary: " + err.Error())
	}

	err = json.Unmarshal(data, summary)
	if err != nil {
		return nil, errors.New("failed to unmarshal campaign summary: " + err.Error())
	}

	return summary, nil
}

type getCampaignCountResponse struct {
	CampaignID        string `json:"campaign_id"`
	CampaignName      string `json:"campaign_name"`
	TotalEmailsSent   int    `json:"total_emails_sent"`
	EmailsRead        int    `json:"emails_read"`
	NewLeadsContacted int    `json:"new_leads_contacted"`
	LeadsReplied      int    `json:"leads_replied"`
	LeadsRead         int    `json:"leads_read"`
}

func (c *Client) GetCampaignCount(campaignId string, startDate time.Time, endDate *time.Time) (count *getCampaignCountResponse, err error) {
	// Convert time.Time to string.
	startDateStr := startDate.Format("01-02-2006")
	endDateStr := ""
	if endDate != nil {
		endDateStr = endDate.Format("01-02-2006")
	}

	queries := []query{
		param("campaign_id", campaignId),
		param("start_date", startDateStr),
	}
	if endDateStr != "" {
		queries = append(queries, param("end_date", endDateStr))
	}

	data, err := c.get("analytics/campaign/count", queries)

	if err != nil {
		return nil, errors.New("failed to get campaign count: " + err.Error())
	}

	err = json.Unmarshal(data, count)
	if err != nil {
		return nil, errors.New("failed to unmarshal campaign count: " + err.Error())
	}

	return count, nil
}

type Lead struct {
	Email           string            `json:"email"`
	FirstName       string            `json:"first_name,omitempty"`
	LastName        string            `json:"last_name,omitempty"`
	CompanyName     string            `json:"company_name,omitempty"`
	Personalization string            `json:"personalization,omitempty"`
	Phone           string            `json:"phone,omitempty"`
	Website         string            `json:"website,omitempty"`
	CustomVariables map[string]string `json:"custom_variables,omitempty"`
}

type addLeadsToCampaignPayload struct {
	CampaignId string `json:"campaign_id"`
	Leads      []Lead `json:"leads"`
}

type addLeadsToCampaignResponse struct {
	Status              string `json:"status"`
	TotalSent           int    `json:"total_sent"`
	LeadsUploaded       int    `json:"leads_uploaded"`
	AlreadyInCampaign   string `json:"already_in_campaign"`
	InvalidEmailCount   string `json:"invalid_email_count"`
	DuplicateEmailCount string `json:"duplicate_email_count"`
	RemainingInPlan     int    `json:"remaining_in_plan"`
}

func (c *Client) AddLeadsToCampaign(campaignId string, leads []Lead) (response *addLeadsToCampaignResponse, err error) {
	payload := addLeadsToCampaignPayload{
		CampaignId: campaignId,
		Leads:      leads,
	}

	data, err := c.post("lead/add", payload)
	if err != nil {
		return nil, errors.New("failed to add leads to campaign: " + err.Error())
	}

	err = json.Unmarshal(data, response)
	if err != nil {
		return nil, errors.New("failed to unmarshal add leads to campaign: " + err.Error())
	}

	return response, nil
}

type internalLead struct {
	Id           string            `json:"id"`
	Timestamp    time.Time         `json:"timestamp_created"`
	Campaign     string            `json:"campaign"`
	Status       int               `json:"status"`
	Contact      string            `json:"contact"`
	EmailOpened  bool              `json:"email_opened"`
	EmailReplied bool              `json:"email_replied"`
	LeadData     map[string]string `json:"lead_data"`
	CampaignName string            `json:"campaign_name"`
}

type getLeadFromCampaignResponse []struct {
	Id           string            `json:"id"`
	Timestamp    string            `json:"timestamp_created"`
	Campaign     string            `json:"campaign"`
	Status       int               `json:"status"`
	Contact      string            `json:"contact"`
	EmailOpened  bool              `json:"email_opened"`
	EmailReplied bool              `json:"email_replied"`
	LeadData     map[string]string `json:"lead_data"`
	CampaignName string            `json:"campaign_name"`
}

func (c *Client) GetLeadFromCampaign(campaignId, email string) (lead internalLead, err error) {
	data, err := c.get("lead/get", []query{param("campaign_id", campaignId), param("email", email)})
	if err != nil {
		return lead, errors.New("failed to get lead from campaign: " + err.Error())
	}

	res := getLeadFromCampaignResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return lead, errors.New("failed to unmarshal get lead from campaign: " + err.Error())
	}

	if len(res) == 0 {
		return lead, errors.New("no lead found")
	}

	if len(res) > 1 {
		return lead, errors.New("multiple leads found")
	}

	// Convert timestamp to time.Time.
	timestamp, err := time.Parse(time.RFC3339, res[0].Timestamp)
	if err != nil {
		return lead, errors.New("failed to parse timestamp: " + err.Error())
	}

	lead = internalLead{
		Id:           res[0].Id,
		Timestamp:    timestamp,
		Campaign:     res[0].Campaign,
		Status:       res[0].Status,
		Contact:      res[0].Contact,
		EmailOpened:  res[0].EmailOpened,
		EmailReplied: res[0].EmailReplied,
		LeadData:     res[0].LeadData,
		CampaignName: res[0].CampaignName,
	}

	return lead, nil
}

type deleteLeadsFromCampaignPayload struct {
	CampaignId           string   `json:"campaign_id"`
	DeleteAllFromCompany bool     `json:"delete_all_from_company"`
	DeleteList           []string `json:"delete_list"`
}
type deleteLeadsFromCampaignResponse struct {
	Status string `json:"status"`
}

func (c *Client) DeleteLeadsFromCampaign(campaignId string, deleteAllFromCompany bool, deleteList []string) error {
	payload := deleteLeadsFromCampaignPayload{
		CampaignId:           campaignId,
		DeleteAllFromCompany: deleteAllFromCompany,
		DeleteList:           deleteList,
	}

	data, err := c.post("lead/delete", payload)
	if err != nil {
		return errors.New("failed to delete leads from campaign: " + err.Error())
	}

	response := deleteLeadsFromCampaignResponse{}
	err = json.Unmarshal(data, &response)
	if err != nil {
		return errors.New("failed to unmarshal delete leads from campaign: " + err.Error())
	}

	if response.Status != "success" {
		return errors.New("failed to delete leads from campaign")
	}

	return nil
}

type updateLeadStatusPayload struct {
	CampaignId string `json:"campaign_id"`
	Email      string `json:"email"`
	NewStatus  string `json:"new_status"`
}

type updateLeadStatusResponse struct {
	Status string `json:"status"`
}

const (
	LeadStatusActive          = "Active"
	LeadStatusCompleted       = "Completed"
	LeadStatusUnsubscribed    = "Unsubscribed"
	LeadStatusInterested      = "Interested"
	LeadStatusMeetingBooked   = "Meeting Booked"
	LeadStatusMeetingComplete = "Meeting Completed"
	LeadStatusClosed          = "Closed"
	LeadStatusOutOfOffice     = "Out of Office"
	LeadStatusNotInterested   = "Not Interested"
	LeadStatusWrongPerson     = "Wrong Person"
)

func (c *Client) UpdateLeadStatus(campaignId, email, status string) error {
	payload := updateLeadStatusPayload{
		CampaignId: campaignId,
		Email:      email,
		NewStatus:  status,
	}

	data, err := c.post("lead/update/status", payload)
	if err != nil {
		return errors.New("failed to update lead status: " + err.Error())
	}

	res := updateLeadStatusResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal update lead status: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to update lead status")
	}

	return nil
}

type updateLeadVariablePayload struct {
	CampaignId string                 `json:"campaign_id"`
	Email      string                 `json:"email"`
	Variables  map[string]interface{} `json:"variables"`
}

type updateLeadVariableResponse struct {
	Status string `json:"status"`
}

func (c *Client) UpdateLeadVariable(campaignId, email string, variables map[string]interface{}) error {
	payload := updateLeadVariablePayload{
		CampaignId: campaignId,
		Email:      email,
		Variables:  variables,
	}

	data, err := c.post("lead/data/update", payload)
	if err != nil {
		return errors.New("failed to update lead variable: " + err.Error())
	}

	res := updateLeadVariableResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal update lead variable: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to update lead variable")
	}

	return nil
}

type setLeadVariablePayload struct {
	CampaignId string                 `json:"campaign_id"`
	Email      string                 `json:"email"`
	Variables  map[string]interface{} `json:"variables"`
}

type setLeadVariableResponse struct {
	Status string `json:"status"`
}

func (c *Client) SetLeadVariable(campaignId, email string, variables map[string]interface{}) error {
	payload := setLeadVariablePayload{
		CampaignId: campaignId,
		Email:      email,
		Variables:  variables,
	}

	data, err := c.post("lead/data/set", payload)
	if err != nil {
		return errors.New("failed to set lead variable: " + err.Error())
	}

	res := setLeadVariableResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal set lead variable: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to set lead variable")
	}

	return nil
}

type deleteLeadVariablesPayload struct {
	CampaignId string   `json:"campaign_id"`
	Email      string   `json:"email"`
	Variables  []string `json:"variables"`
}

type deleteLeadVariablesResponse struct {
	Status string `json:"status"`
}

func (c *Client) DeleteLeadVariables(campaignId, email string, variables []string) error {
	payload := deleteLeadVariablesPayload{
		CampaignId: campaignId,
		Email:      email,
		Variables:  variables,
	}

	data, err := c.post("lead/data/update", payload)
	if err != nil {
		return errors.New("failed to delete lead variables: " + err.Error())
	}

	res := deleteLeadVariablesResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal delete lead variables: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to delete lead variables")
	}

	return nil
}

type addEntriesToBlocklistPayload struct {
	Entries []string `json:"entries"`
}

type addEntriesToBlocklistResponse struct {
	Status             string `json:"status"`
	EntriesAdded       int    `json:"entries_added"`
	AlreadyInBlocklist int    `json:"already_in_blocklist"`
	BlocklistId        string `json:"blocklist_id"`
}

func (c *Client) AddEntriesToBlocklist(entries []string) (entriesAdded int, err error) {
	payload := addEntriesToBlocklistPayload{
		Entries: entries,
	}

	data, err := c.post("blocklist/add/entries", payload)
	if err != nil {
		return 0, errors.New("failed to add entries to blocklist: " + err.Error())
	}

	res := addEntriesToBlocklistResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return 0, errors.New("failed to unmarshal add entries to blocklist: " + err.Error())
	}

	if res.Status != "success" {
		return 0, errors.New("failed to add entries to blocklist")
	}

	return res.EntriesAdded, nil
}

type listAccountsResponse struct {
	Status   string `json:"status"`
	Accounts []struct {
		Email            string   `json:"email"`
		TimestampCreated string   `json:"timestamp_created"`
		TimestampUpdated string   `json:"timestamp_updated"`
		Payload          *Payload `json:"payload"`
	} `json:"accounts"`
}

type Payload struct {
	Name struct {
		Last  string `json:"last"`
		First string `json:"first"`
	} `json:"name"`
	Warmup struct {
		Limit    int `json:"limit"`
		Advanced struct {
			WarmCtd        bool `json:"warm_ctd"`
			OpenRate       int  `json:"open_rate"`
			WeekdayOnly    bool `json:"weekday_only"`
			ImportantRate  int  `json:"important_rate"`
			ReadEmulation  bool `json:"read_emulation"`
			SpamSaveRate   int  `json:"spam_save_rate"`
			RandomRangeMin int  `json:"random_range_min"`
			RandomRangeMax int  `json:"random_range_max"`
		} `json:"advanced"`
		Increment int `json:"increment"`
		ReplyRate int `json:"reply_rate"`
	} `json:"warmup"`
	ImapHost   string `json:"imap_host"`
	ImapPort   int    `json:"imap_port"`
	SmtpHost   string `json:"smtp_host"`
	SmtpPort   string `json:"smtp_port"`
	DailyLimit int    `json:"daily_limit"`
	SendingGap string `json:"sending_gap"`
}

type Account struct {
	Email            string
	TimestampCreated time.Time
	TimestampUpdated time.Time
	Payload          *Payload
}

func (c *Client) ListAccounts(limit, skip int) ([]Account, error) {
	data, err := c.get("account/list", []query{
		param("limit", strconv.Itoa(limit)),
		param("skip", strconv.Itoa(skip)),
	})
	if err != nil {
		return nil, errors.New("failed to list accounts: " + err.Error())
	}

	res := listAccountsResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return nil, errors.New("failed to unmarshal list accounts: " + err.Error())
	}

	if res.Status != "success" {
		return nil, errors.New("failed to list accounts")
	}

	accounts := make([]Account, len(res.Accounts))
	for i, account := range res.Accounts {
		timestampCreated, err := time.Parse(time.RFC3339, account.TimestampCreated)
		if err != nil {
			return nil, errors.New("failed to parse timestamp created: " + err.Error())
		}

		timestampUpdated, err := time.Parse(time.RFC3339, account.TimestampUpdated)
		if err != nil {
			return nil, errors.New("failed to parse timestamp updated: " + err.Error())
		}

		accounts[i] = Account{
			Email:            account.Email,
			TimestampCreated: timestampCreated,
			TimestampUpdated: timestampUpdated,
			Payload:          account.Payload,
		}
	}

	return accounts, nil
}

type checkAccountVitalsPayload struct {
	Accounts []string `json:"accounts"`
}

type checkAccountVitalsResponse struct {
	Status      string          `json:"status"`
	SuccessList []AccountVitals `json:"success_list"`
	FailureList []AccountVitals `json:"failure_list"`
}

type AccountVitals struct {
	Domain string
	Mx     bool
	Spf    bool
	Dkim   bool
	Dmarc  bool
}

func (c *Client) CheckAccountVitals(accounts []string) (successList, failureList []AccountVitals, err error) {
	payload := checkAccountVitalsPayload{
		Accounts: accounts,
	}

	data, err := c.post("account/test/vitals", payload)
	if err != nil {
		return nil, nil, errors.New("failed to check account vitals: " + err.Error())
	}

	res := checkAccountVitalsResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return nil, nil, errors.New("failed to unmarshal check account vitals: " + err.Error())
	}

	if res.Status != "success" {
		return nil, nil, errors.New("failed to check account vitals")
	}

	successList = make([]AccountVitals, len(res.SuccessList))
	for i, account := range res.SuccessList {
		successList[i] = AccountVitals{
			Domain: account.Domain,
			Mx:     account.Mx,
			Spf:    account.Spf,
			Dkim:   account.Dkim,
			Dmarc:  account.Dmarc,
		}
	}

	failureList = make([]AccountVitals, len(res.FailureList))
	for i, account := range res.FailureList {
		failureList[i] = AccountVitals{
			Domain: account.Domain,
			Mx:     account.Mx,
			Spf:    account.Spf,
			Dkim:   account.Dkim,
			Dmarc:  account.Dmarc,
		}
	}

	return successList, failureList, nil
}

type enableWarmupPayload struct {
	Email string `json:"email"`
}

type enableWarmupResponse struct {
	Status string `json:"status"`
}

func (c *Client) EnableWarmup(email string) error {
	payload := enableWarmupPayload{
		Email: email,
	}

	data, err := c.post("account/warmup/enable", payload)
	if err != nil {
		return errors.New("failed to enable warmup: " + err.Error())
	}

	res := enableWarmupResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal enable warmup: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to enable warmup")
	}

	return nil
}

type pauseWarmupPayload struct {
	Email string `json:"email"`
}

type pauseWarmupResponse struct {
	Status string `json:"status"`
}

func (c *Client) PauseWarmup(email string) error {
	payload := pauseWarmupPayload{
		Email: email,
	}

	data, err := c.post("account/warmup/pause", payload)
	if err != nil {
		return errors.New("failed to pause warmup: " + err.Error())
	}

	res := pauseWarmupResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal pause warmup: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to pause warmup")
	}

	return nil
}

type markAccountAsFixedPayload struct {
	Email string `json:"email,omitempty"`
}

type markAccountAsFixedResponse struct {
	Status string `json:"status"`
}

func (c *Client) MarkAccountAsFixed(email string) error {
	payload := markAccountAsFixedPayload{
		Email: email,
	}

	data, err := c.post("account/mark_fixed", payload)
	if err != nil {
		return errors.New("failed to mark accounts as fixed: " + err.Error())
	}

	res := markAccountAsFixedResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal mark accounts as fixed: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to mark accounts as fixed")
	}

	return nil
}

func (c *Client) MarkAllAccountsAsFixed() error {
	payload := markAccountAsFixedPayload{}

	data, err := c.post("account/mark_fixed", payload)
	if err != nil {
		return errors.New("failed to mark accounts as fixed: " + err.Error())
	}

	res := markAccountAsFixedResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal mark accounts as fixed: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to mark accounts as fixed")
	}

	return nil
}

type deleteAccountPayload struct {
	Email string `json:"email"`
}

type deleteAccountResponse struct {
	Status string `json:"status"`
}

func (c *Client) DeleteAccount(email string) error {
	payload := deleteAccountPayload{
		Email: email,
	}

	data, err := c.post("account/delete", payload)
	if err != nil {
		return errors.New("failed to delete account: " + err.Error())
	}

	res := deleteAccountResponse{}
	err = json.Unmarshal(data, &res)
	if err != nil {
		return errors.New("failed to unmarshal delete account: " + err.Error())
	}

	if res.Status != "success" {
		return errors.New("failed to delete account")
	}

	return nil
}
