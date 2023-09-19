package main

import(
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Accounts contains the map used to set the usernames. Make sure to
// lock and unlock as appropriate.
type Accounts struct {
	sync.RWMutex

	// uris is a map, mapping account names likes
	// "@alex@alexschroeder.ch" to URIs like
	// "https://social.alexschroeder.ch/@alex".
	uris map[string]string
}

// accounts holds the global mapping of accounts to profile URIs.
var accounts Accounts

// initAccounts sets up the accounts map. This is called once at
// startup and therefore does not need to be locked. On ever restart,
// this map starts empty and is slowly repopulated as pages are
// visited.
func initAccounts() {
	accounts.uris = make(map[string]string)
}

// account links a social media account like @account@domain to a
// profile page like https://domain/user/account. Any account seen for
// the first time uses a best guess profile URI. It is also looked up
// using webfinger, in parallel. See lookUpAccountUri. If the lookup
// succeeds, the best guess is replaced with the new URI so on
// subsequent requests, the URI is correct.
func account(p *parser.Parser, data []byte, offset int) (int, ast.Node) {
	data = data[offset:]
	i := 1 // skip @ of username
	n := len(data)
	d := 0
	for i < n && (
		data[i] >= 'a' && data[i] <= 'z' ||
		data[i] >= 'A' && data[i] <= 'Z' ||
		data[i] >= '0' && data[i] <= '9' ||
		data[i] == '@' ||
		data[i] == '.' ||
		data[i] == '-') {
		if data[i] == '@' {
			if d != 0 {
				// more than one @ is invalid
				return 0, nil
			} else {
				d = i+1 // skip @ of domain
			}
		}
		i++
	}
	for i > 1 && (
		data[i-1] == '.' ||
		data[i-1] == '-') {
		i--
	}
	if i == 0 || d == 0 {
		return 0, nil
	}
	user := data[0:d-1] // includes @
	domain := data[d:i] // excludes @
	account := data[1:i] // excludes @
	accounts.RLock()
	uri, ok := accounts.uris[string(account)]
	defer accounts.RUnlock()
	if !ok {
		fmt.Printf("Looking up %s\n", account)
		uri = "https://" + string(domain) + "/users/" + string(user[1:])
		accounts.uris[string(account)] = uri // prevent more lookings
		go lookUpAccountUri(string(account), string(domain))
	}
	link := &ast.Link{
		Destination: []byte(uri),
		Title:       data[0:i],
	}
	ast.AppendChild(link, &ast.Text{Leaf: ast.Leaf{Literal: data[0:d-1]}})
	return i, link
}

// lookUpAccountUri is called for accounts that haven't been seen
// before. It calls webfinger and parses the JSON. If possible, it
// extracts the link to the profile page and replaces the entry in
// accounts.
func lookUpAccountUri(account, domain string) {
	uri := "https://" + domain + "/.well-known/webfinger"
	resp, err := http.Get(uri + "?resource=acct:" + account)
	if err != nil {
		fmt.Printf("Failed to look up %s: %s\n", account, err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read from %s: %s\n", account, err)
		return
	}
	var wf WebFinger
	err = json.Unmarshal([]byte(body), &wf)
	if err != nil {
		fmt.Printf("Failed to parse the JSON from %s: %s\n", account, err)
		return
	}
	uri, err = parseWebFinger(body)
	if err != nil {
		fmt.Printf("Could not find profile URI for %s: %s\n", account, err)
	}
	fmt.Printf("Found profile for %s: %s\n", account, uri)
	accounts.Lock()
	defer accounts.Unlock()
	accounts.uris[account] = uri
}

// Link a link in the WebFinger JSON.
type Link struct {
	Rel string `json:"rel"`
	Type string `json:"type"`
	Href string `json:"href"`
}

// WebFinger is a structure used to unmarshall JSON.
type WebFinger struct {
	Subject string `json:"subject"`
	Aliases []string `json:"aliases"`
	Links []Link `json:"links"`
}

// parseWebFinger parses the web finger JSON and returns the profile
// page URI. For unmarshalling the JSON, it uses the Link and
// WebFinger structs.
func parseWebFinger(body []byte) (string, error) {
	var wf WebFinger
	err := json.Unmarshal(body, &wf)
	if err != nil {
		return "", err
	}
	for _, link := range wf.Links {
		if link.Rel == "http://webfinger.net/rel/profile-page" &&
			link.Type == "text/html" {
			return link.Href, nil
		}
	}
	return "", err
}
