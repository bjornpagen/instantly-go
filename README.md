# instantly-go

Instantly-Go is a Go library for accessing the Instantly.ai API, making it easier for you to manage campaigns, leads, accounts, and other resources. The library provides easy-to-use interfaces for interacting with the Instantly service, allowing you to focus on building your applications.

## Features

- Manage campaigns: create, list, modify, and delete campaigns
- Manage leads: add, update, and delete leads from campaigns
- Manage accounts: list, check vitals, and manage warmup status
- Flexible configuration: set custom host, API version, rate limit, and HTTP client

## Installation

```sh
go get github.com/bjornpagen/instantly-go
```

## Usage

Import the package:

```go
import instantly "github.com/bjornpagen/instantly-go"
```

Create a new client with your API key:

```go
client := instantly.New("your_api_key")
```

## Examples

List Campaigns

```go
campaigns, err := client.ListCampaigns()
if err != nil {
    log.Fatal(err)
}
fmt.Println(campaigns)
```

Add Leads to Campaign

```go
leads := []instantly.Lead{
    {Email: "email@example.com", FirstName: "John", LastName: "Doe"},
    {Email: "another-email@example.com", FirstName: "Jane", LastName: "Smith"},
}

resp, err := client.AddLeadsToCampaign("campaign_id", leads)
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp)
```

## Documentation

For detailed documentation and available methods, please refer to the Godoc.

## Contributing

Please feel free to open an issue or submit a pull request if you'd like to contribute to the project. All contributions are welcome!

## License

instantly-go is released under the [Zero-Clause BSD License](https://opensource.org/license/0bsd/).