package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"github.com/gorilla/websocket"
	"html/template"
	"log"
	"net/http"
	"html"
	"time"
	"strings"
)

var templates = template.Must(template.ParseFiles("sample.html", "samplep.html", "index.html"))

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateRandomString(length int) string {
    seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
    b := make([]byte, length)
    for i := range b {
        b[i] = charset[seededRand.Intn(len(charset))]
    }
    return string(b)
}

// A 'command' type
type MessageCmd struct {
	Key   string
	Value string
}

func ReadMessageCmd(msg string) (MessageCmd, error) {
	parts := strings.SplitN(msg, "\x00", 2)
	if len(parts) != 2 {
		return MessageCmd{}, fmt.Errorf("invalid payload, expected two strings separated by \\0")
	}
	return MessageCmd{Key: parts[0], Value: parts[1]}, nil
}

// Message entity
type Message struct {
	Msg  string
	From string // Always a user, so no 'user_' prefix, or nothing, representing 'system'
	To   string // Can be user or group, so there will be prefix
	Ts   time.Time
}

type Group struct {
	Name    string    // duplicated when assigned as key
	History []Message // To must be the name of this
	Members map[string]bool
	// A 'rebuildable' string representing current message history to print to client
	Output string
}

// Methods of a Group

// Adds the message only if the timestamp is > the latest timestamp (not an error)
// Useful for doing this asynchronously to prevent duplications (I think)
// This function doesnot notify the users though (no access to the stuff
func (room *Group) AddMessage(msg Message) string {
	//if msg.Ts > room.History[len(room.History)-1].Ts {
	if (len(room.History) == 0) || msg.Ts.After(room.History[len(room.History)-1].Ts) {
		room.History = append(room.History, msg)
		room.Output = room.Output + "\n" + msg.From + " : " + msg.Msg
	}
	return room.Output
}

// Client entity
type Client struct {
	// channel where others send stuff (other endpoint is in the goroutine for client)
	MesgC       chan<- Message
	Name        string            // duplicated later when assigned as keys to 'Clients' of context
	Chats       map[string]bool // names of the groups and friends
	ActiveGroup string            // name of the currently active group or friend of chatbox
	// 'ActiveGroup' might be "", that is a 'self message' like reminder
	// A special group, singifying self's private messages
	SelfGroup Group
}

// The 'App context' that contains the whole thing
type Context struct {
	// Be careful, when updating name, need to 'remove' and 'add' again the entries
	// all clients' map
	Clients map[string]*Client
	// all groups' map
	Groups map[string]*Group
}

func (cxt *Context) DumpIt() {

	fmt.Println("Context = ", *cxt)
	fmt.Print("Clients = ")
	for n,u := range cxt.Clients{
		fmt.Print(n, " => ", u)
	}
	fmt.Println()
	fmt.Print("Groups = ")
	for n,g := range cxt.Groups{
		fmt.Print(n, " => ",  g)
	}
	fmt.Println()
}

func (cxt *Context) AddClient(client *Client) error {
	if client == nil {
		return errors.New("Need to provide a valid client to add it")
	}
	// Check if the client was already there
	_, ok := cxt.Clients[client.Name]
	if ok {
		return errors.New("Client already present")
	}
	cxt.Clients[client.Name] = client
	return nil
}
func (cxt *Context) AddGroup(group *Group) error {
	if group == nil {
		return errors.New("Need to provide a valid group to add it")
	}
	// Check if the group was already there
	_, ok := cxt.Groups[group.Name]
	if ok {
		return errors.New("Group already present")
	}
	cxt.Groups[group.Name] = group
	return nil
}
func (cxt *Context) RenameClient(oldname string, newname string) (*Client, error) {
	client, ok := cxt.Clients[oldname]
	if !ok {
		return nil, errors.New("Cannot rename unregistered client")
	}
	client.Name = newname
	delete(cxt.Clients, oldname)
	cxt.Clients[newname] = client

	return client, nil
}
func (cxt *Context) RenameGroup(oldname string, newname string) (*Group, error) {
	group, ok := cxt.Groups[oldname]
	if !ok {
		return nil, errors.New("Cannot rename unregistered group")
	}
	group.Name = newname
	delete(cxt.Groups, oldname)
	cxt.Groups[newname] = group

	return group, nil
}
// TODO:: Later also make it that it resolves dms also
func (cxt *Context) ResolveGroup(usr *Client, grpname string) (*Group, error){
	if grpname == "" {
		return &usr.SelfGroup, nil
	} else if grp, ok := cxt.Groups[grpname]; ok {
		return grp, nil
	}
	return nil, errors.New("No groups found for name " + grpname)
}

// Helper fxns that just send websocket messages
// To be called by respective goroutines of own client
// Will always send escaped html string directly

// Updates the chat content of the client's UI
func (usr *Client) update_chat(conn *websocket.Conn, cxt *Context) error {
	//var grp *Group = nil
	grp, err := cxt.ResolveGroup(usr, usr.ActiveGroup)
	if err != nil{
		return errors.New("Active Group Unavailable")
	}
	// Convert each '\n' into <br/>
	msg := "chatbox" + "\x00" +
		strings.Replace(html.EscapeString(grp.Output), "\n", "<br/>", -1)
		
	return conn.WriteMessage(websocket.TextMessage, []byte(msg))
}
func (usr *Client) send_notification(conn *websocket.Conn, message string) error {
	// Need to make a mechanism for sending 'delete' for the notificaiton also
	msg := "notifbox" + "\x00" + html.EscapeString(message)

	// TODO:: After a certain time, also send info to delete the contents for the notification

	return conn.WriteMessage(websocket.TextMessage, []byte(msg))
}
func (usr *Client) update_groups(conn *websocket.Conn) error {
	msg := "roomlist" + "\x00"
	for grp, _ := range usr.Chats {
		msg = msg + "<option value='" + grp + "'> " + grp + "</option>"
	}
	return conn.WriteMessage(websocket.TextMessage, []byte(msg))
}
func (usr *Client) update_name(conn *websocket.Conn) error {
	msg := "username" + "\x00" + html.EscapeString(usr.Name)
	return conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// This will launch the client as goroutine inside
// key of 'clients' is also the 'Name' of the client (cuz me lazy)
func (cxt *Context) launch_client(name string, conn *websocket.Conn) error {
	// Channel for notifications for this client
	notch := make(chan Message)
	// Client struct created here and consumed as closure captures later inside here
	client := Client{
		MesgC: notch, Name: name, ActiveGroup: "",
		SelfGroup: Group{Name: "", History:[]Message{}, Members:map[string]bool{}, Output:""},
		Chats: map[string]bool{},
	}

	err := cxt.AddClient(&client)
	if err != nil {
		return err
	}


	// channel and goroutine for selecting on io
	closed := make(chan interface{})

	// A special channel that is used to prevent 'concurrent writes'
	comm_ch := make(chan func())

	
	// Goroutine that handles notificaitions
	go func(notch <-chan Message) {
		defer conn.Close()

		// Receive a 'Message' struct to notify the client
		//  it could have been sent by anyone, but ultimately its simple to handle

		// Wait until either someone sends stuff on channel (notification)
		//  or the connection is closed

		for {
			select {
			case notif := <-notch:
				if client.ActiveGroup == notif.To {
					// If the current group is relevant, then also update it
					_ = client.update_chat(conn, cxt)
				} else {

					// Just send the notification
					_ = client.send_notification(conn, "("+notif.To+")"+
						notif.From+":"+notif.Msg)
					// update the 'notification' box
				}
			case fn := <- comm_ch:
				fn()
			case <- closed:
				return
			}
		}
	}(notch)
	comm_ch <- func(){_=client.update_name(conn)}
	// Goroutine that handles messages and client commands
	go func() {
		defer func() { closed <- true}()
		for {
			_, msg, err := conn.ReadMessage()
			// If err, like due to ws close, break
			if err != nil {
				break
			}

			// Parse the msg to get the destination
			cl_info, err := ReadMessageCmd(string(msg))
			if err == nil {
				// Handle according to the type of the 'Key'
				// Action types:
				// Simple message action (forward them)
				// Rename yourself (change directly, forward to all groups also)
				// Change Active group (validate and change directly)
				// Add / Create group/friend
				//   (adding updates entry to that chat room and self)
				//   (creating also 'creates' the chat room)

				switch cl_info.Key {
				case "message":
					msg := Message{Msg: cl_info.Value, From: client.Name, To: client.ActiveGroup, Ts: time.Now()}
					//TODO:: Data race might still happen
					// TODO:: Handle dms later, for now all are grp
					
					sel_grp,_ := cxt.ResolveGroup(&client, client.ActiveGroup)
					_=sel_grp.AddMessage(msg)
					// forward the messages (notify/update the clients)
					for usrname, _ := range sel_grp.Members {
						usr := cxt.Clients[usrname]
						usr.MesgC <- msg
					}

				case "rename":
					// Change value in 'self'
					oldname := client.Name
					//TODO:: Data race might happen
					cxt.RenameClient(oldname, cl_info.Value)
					// Find what other groups is this involved in

					// The task is to update the groups (maybe not history)
					//   then to update the active users somehow involved
					//   with the user

					// You can do, A> collect the users
					//             B> If the active group is relevant, then
					//                update, otherwise, be silent

					// A 'set' of the 'affected users'
					a_users := make(map[*Client]bool)
					// Fill with only true values,
					// when indexing, if true, exists

					for grpname, _ := range client.Chats {

						// Rename this from the 'Members'
						// For each group, find what online clients are
						//   actively chatting there
						// TODO:: Handle error here
						grp,_ := cxt.ResolveGroup(&client, grpname)

						// Add the new name directly here
						//   Remove the old name later
						grp.Members[client.Name] = true
						delete(grp.Members, oldname)
						for usrname, _ := range grp.Members {
							
							// If this is the active group of that
							// also add into affected users
							// TODO:: Also handle dms later

							if grp.Name == cxt.Clients[usrname].ActiveGroup {
								a_users[cxt.Clients[usrname]] = true
							}

						}

					}
					// A Message to represent trigger of this action
					msg := Message{
						Msg:  "Renamed `" + oldname + "` to `" + client.Name + "`",
						From: "",
						To:   "",
						Ts:   time.Now(),
					}
					// Now for each affected user, send notification
					for usr, _ := range a_users {
						// TODO:: also later decide if to update chat
						usr.MesgC <- msg
					}
					comm_ch <- func(){_=client.update_name(conn)}

				case "change_group":
					// Just change 'active group'
					client.ActiveGroup = cl_info.Value

					// Update UI's chat entry
					comm_ch <- func(){_ = client.update_chat(conn, cxt)}
				// TODO:: Need to handle this (maybe)

				case "create_group":
					// (name = 'grp:<user given name>')
					gname := "grp:" + cl_info.Value

					// if name is not already used, then only create it
					_, ok := cxt.Groups[gname]
					if !ok {
						// Add this user to that group
						grp := Group{
							Name:    gname,
							History: []Message{},
							Output: "",
							Members: map[string]bool{client.Name: true},
						}
						cxt.Groups[gname] = &grp

						// Change the active group
						client.Chats[gname] = true
						client.ActiveGroup = gname

						// Update chat box
						comm_ch<-func(){_=client.update_chat(conn, cxt)}
						comm_ch<-func(){_=client.update_groups(conn)}
						//TODO:: Need to handle updating chat
					}
				case "add_group":
					// Search for the group, again
					//   (name = 'grp:<user given name>')
					gname := "grp:" + cl_info.Value

					// If found, add user if not added, and notify them
					grp, ok := cxt.Groups[gname]
					if ok {
						// TODO:: Handle this race condition later
						grp.Members[client.Name] = true

						// update active group
						client.Chats[gname] = true
						client.ActiveGroup = gname

						// Update chat box
						comm_ch<-func(){_ = client.update_chat(conn, cxt)}
						comm_ch<-func(){_ = client.update_groups(conn)}
						//TODO:: Need to handle updating chat
					}

				case "add_friend":
					panic("Ive not implemented DMs yet !")
					// Search for the friend (name = 'user:<user given name>')
					uname := "user:" + cl_info.Value

					// If found, add user if not added, and notify them
					_, ok := cxt.Clients[uname]
					if ok {
						// TODO:: Not handled dm as of yet
						// Add friend is like adding group somehow

						// update active group
						//client.ActiveGroup = uname

						// If found, and not friend already, add as friend

						// also send notification to that friend

						// update active group

						comm_ch<-func(){_ = client.update_chat(conn, cxt)}
						comm_ch<-func(){_ = client.update_groups(conn)}

						// update chat box

						// TODO:: Handle invalid ones also
					}
				}
			}
		}
	}()
	return nil
}

func main() {
	fmt.Println("Hello, World\n")
	pfn := func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Method: `%v`, URL: `%v`, Host: `%v`, Form: `%v`, PostForm: `%v`, RemoteAddr: `%v`, RequestURI: `%v`, Pattern: `%v`\n", r.Method, r.URL, r.Host, r.Form, r.PostForm, r.RemoteAddr, r.RequestURI, r.Pattern)
	}

	// Makes a func that 'locks' in the endpoint
	handle_locked := func(endpt string, fn func(w http.ResponseWriter, r *http.Request)) {
		http.HandleFunc(endpt, fn)
		if endpt[len(endpt)-1] == '/' {
			http.HandleFunc(endpt+"{$}", fn)
		} else {
			http.HandleFunc(endpt+"/{$}", fn)
		}
	}
	_ = handle_locked

	http.HandleFunc("GET /hi", func(w http.ResponseWriter, r *http.Request) {
		t, _ := template.ParseFiles("sample.html")
		t.Execute(w, nil)
		pfn(w, r)
	})
	//handle_locked("POST /hi", func (w http.ResponseWriter, r *http.Request) {
	http.HandleFunc("POST /hi", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"apple": 56, "banana": 69})
		pfn(w, r)
	})

	var wsupgrader = websocket.Upgrader{}
	cxt := Context{Clients:map[string]*Client{}, Groups:map[string]*Group{}}
	handle_locked("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsupgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		fmt.Println("Started a new ws connection")
		// TODO:: Handle error
		_=cxt.launch_client( generateRandomString(6), conn)
			
		/* go func() {
			defer conn.Close()

			defer fmt.Println("Closing the ws connection")
			for i := 0; i < 10; i++ {
				mt, msg, err := conn.ReadMessage()
				if err != nil {
					fmt.Println("There was error ", err)
				} else {
					fmt.Println("The client sent message of type ", mt, " as `", string(msg), "` from websocket")

					// Read the json
					cl_info := ClientCmd{}
					json.Unmarshal(msg, &cl_info)

					// Create the reply
					sr_ans := ServerCmd{
						Id:   "box1",
						Text: "You have messaged @" + string(cl_info.Tag) + " `" + string(cl_info.Payload) + "`",
					}
					if cl_info.Tag != "everyone" {
						sr_ans.Id = "box2"
					}
					wr, err := conn.NextWriter(websocket.TextMessage)
					if err == nil {
						json.NewEncoder(wr).Encode(sr_ans)
					}
					wr.Close()
				}
			}
		}()
	})*/
	})

	//http.HandleFunc("POST /hi/{$}", func (w http.ResponseWriter, r *http.Request){
	//	//t, _ := template.ParseFiles("samplep.html")
	//	//t.Execute(w, nil)
	//	w.Header().Set("Content-Type", "application/json")
	//	json.NewEncoder(w).Encode( map[string]int{ "apple": 56, "banana": 69, } )
	//	pfn(w,r)
	//})
	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		t, _ := template.ParseFiles("index.html")
		t.Execute(w, nil)
		pfn(w, r)
	})

	log.Fatal(http.ListenAndServe(":6699", nil))

}
