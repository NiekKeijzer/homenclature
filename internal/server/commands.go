package server

import "strings"

// Commands: typing ">" in the search pill switches it from search to a list
// of commands. They all run on the page. Commands can be grouped: ">" alone
// lists the top level, where a command opens each group, and typing searches
// all of them. The first group manages the user's own API keys (keys.go).

// command is one row in the command list: it opens a group, changes a key,
// or moves all keys.
type command struct {
	Title  string
	Detail string
	Group  string // the group it's in; empty: the top level
	Opens  string // the group it opens
	App    string // title of the app it's for; empty if none
	Icon   string // that app's icon
	Key    string // searchKey of the app whose key it changes
	Action string // "save" (asks for the key first), "remove", or "server" or "browser" to move all keys
	Help   string // for save: where to make a key
}

// commandList is the command list, with what the last command did.
type commandList struct {
	Items   []command
	Message string
}

const keysGroup = "keys"

// commands returns the user's commands; nil if they have none.
func (a *app) commands(s session, k userKeys) []command {
	apps := a.keyApps(s, k.Keys)
	if len(apps) == 0 {
		return nil
	}
	var names []string
	var group []command
	for _, k := range apps {
		names = append(names, k.Link.Title)
		c := command{Group: keysGroup, App: k.Link.Title, Icon: k.Link.Icon, Key: k.Key, Action: "save", Help: k.Help}
		if !k.Own {
			c.Title, c.Detail = "Use your own "+c.App+" key", "Using the shared key"
			group = append(group, c)
			continue
		}
		c.Title, c.Detail = "Replace your "+c.App+" key", "Using your key"
		remove := c
		remove.Title, remove.Detail, remove.Action, remove.Help = "Remove your "+c.App+" key", "Search "+c.App+" with the shared key again", "remove", ""
		group = append(group, c, remove)
	}
	// With a key store, each person chooses where their keys are kept.
	if a.store != nil {
		move := command{Group: keysGroup, Title: "Keep your keys on all your devices", Detail: "Kept in this browser only", Action: "server"}
		if k.OnServer {
			move.Title, move.Detail, move.Action = "Keep your keys in this browser only", "Kept on all your devices", "browser"
		}
		group = append(group, move)
	}
	manage := command{Title: "Manage your keys", Detail: strings.Join(names, ", "), Opens: keysGroup}
	return append([]command{manage}, group...)
}
