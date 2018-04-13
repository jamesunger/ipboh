/*
*
* Copyright 2015 James Unger
*
* This program is free software: you can redistribute it and/or modify
*    it under the terms of the GNU General Public License as published by
*    the Free Software Foundation, either version 3 of the License, or
*    (at your option) any later version.
*
*    This program is distributed in the hope that it will be useful,
*    but WITHOUT ANY WARRANTY; without even the implied warranty of
*    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
*    GNU General Public License for more details.
*
*    You should have received a copy of the GNU General Public License
*    along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/ipfs/go-ipfs/blocks/key"
	net "gx/ipfs/QmVCe3SNMjkcPgnpFhZs719dheq6xE7gJwjzV7aWcUM4Ms/go-libp2p/p2p/net"
	core "github.com/ipfs/go-ipfs/core"
	corenet "github.com/ipfs/go-ipfs/core/corenet"
	coreunix "github.com/ipfs/go-ipfs/core/coreunix"
	fsrepo "github.com/ipfs/go-ipfs/repo/fsrepo"
	"github.com/pivotal-golang/bytefmt"
	"golang.org/x/crypto/openpgp"
	peer "gx/ipfs/QmRBqJF7hb8ZSpRcMwUt8hNhydWcxGEhtk81HKq6oUwKvs/go-libp2p-peer"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/VividCortex/godaemon"
	config "github.com/ipfs/go-ipfs/repo/config"
	"golang.org/x/net/context"
)

const HEADER_SIZE = 120
const MAXTRIES = 10
const REQUIREDPEERS = 3

type IpbohConfig struct {
	Serverhash string
	Port       int
	Autoenc	   string // gpg recipient to auto encrypt with
}

type Index struct {
	Entries   []*Entry
	WriteList []string `json:"-"`
	ReadList  []string `json:"-"`
}

type Entry struct {
	Name      string
	Hash      string
	Timestamp time.Time
	Size      int
}

func handleIndex(n *core.IpfsNode, ctx context.Context, index *Index, getCurIndex func() *Index) {
	list, err := corenet.Listen(n, "/pack/index")
	if err != nil {
		panic(err)
	}

	for {
		fmt.Printf("Waiting for index (ls) requests: %s\n", n.Identity)
		con, err := list.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}

		go func() {
			defer con.Close()
			index = getCurIndex()
			fmt.Printf("Connection from: %s\n", con.Conn().RemotePeer())
			if len(index.ReadList) != 0 {
				present := false
				for i := range index.ReadList {
					fmt.Println(index.ReadList[i])
					if con.Conn().RemotePeer().Pretty() == index.ReadList[i] {
						present = true
						break
					}
				}

				if !present {
					fmt.Println("Not on read list", con.Conn().RemotePeer().Pretty())
				} else {
					fmt.Println("On read list", con.Conn().RemotePeer().Pretty())
				}

			}

			indexbytes, err := json.Marshal(index)
			if err != nil {
				panic(err)
			}
			count, err := con.Write(indexbytes)
			if err != nil {
				fmt.Println(err)
			}
			fmt.Println("Wrote bytes:", count)
			//con.Close()
		}()
	}


}

func handleAdd(n *core.IpfsNode, ctx context.Context, index *Index, mtx *sync.Mutex, dspath string, reloadindex chan *Entry, getCurIndex func() *Index) {
	list, err := corenet.Listen(n, "/pack/add")
	if err != nil {
		panic(err)
	}
	//fmt.Printf("I am ready to add: %s\n", n.Identity)

	for {
		fmt.Println("Waiting for add...")
		con, err := list.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}

		go func() {
			defer con.Close()

			index = getCurIndex()
			fmt.Printf("Connection from: %s\n", con.Conn().RemotePeer())
			if len(index.WriteList) != 0 {
				present := false
				for i := range index.WriteList {
					if con.Conn().RemotePeer().Pretty() == index.WriteList[i] {
						present = true
						break
					}
				}

				if !present {
					fmt.Println("Not on write list", con.Conn().RemotePeer().Pretty())
				} else {
					fmt.Println("On write list", con.Conn().RemotePeer().Pretty())
				}

			}

			serverReader := &serverContentReader{r: con}

			fmt.Println("Add request:", serverReader.Name())
			key, err := coreunix.Add(n, serverReader)
			if err != nil {
				panic(err)
			}

			fmt.Println("Added:", key)
			entry := Entry{Timestamp: time.Now(), Size: serverReader.n - HEADER_SIZE, Name: serverReader.Name(), Hash: key}

			reloadindex <- &entry
		}()

	}


}

func MyDial(n *core.IpfsNode, target peer.ID, protoid string) (net.Stream,error) {

	var con net.Stream
	var err error
	for i := 0; i < MAXTRIES; i++ {
		con, err = corenet.Dial(n, target, protoid)
		if err != nil && i > MAXTRIES {
			fmt.Fprintln(os.Stderr, "Dial failed to connect, tried",i,"times. Giving up.")
			fmt.Fprintln(os.Stderr, fmt.Sprintf("%s",err))
			return nil,err
		} else if err != nil {
			var sleeptime time.Duration
			sleeptime = time.Duration(i)
			time.Sleep(sleeptime*time.Second)
			fmt.Fprintln(os.Stderr, "Dial failed, retrying:",err)
		} else if err == nil {
			return con,err
		}
	}

	return con,err

}

func getEntryList(n *core.IpfsNode, target peer.ID) *Index {

	index := makeIndex()
	con, err := MyDial(n, target, "/pack/index")
	if err != nil {
		fmt.Println(err)
		return index
	}

	rawbytes, err := ioutil.ReadAll(con)
	if err != nil {
		fmt.Println(err)
		return index
	}

	err = json.Unmarshal(rawbytes, index)
	if err != nil {
		fmt.Println(err)
		return index
	}

	return index

}

func findKey(keyring openpgp.EntityList, name string) *openpgp.Entity {
	for _, entity := range keyring {
		for _, ident := range entity.Identities {
			if strings.Contains(ident.Name, name) {
				return entity
			}
		}
	}

	return nil
}

/*
func decryptOpenpgp(data io.Reader, gpghome string, pass []byte, isarmored bool) (io.Reader, error) {
	privkeyfile, err := os.Open(fmt.Sprintf("%s%ssecring.gpg", gpghome, string(os.PathSeparator)))
	if err != nil {
		fmt.Println("Failed to open secring", err)
		return nil, err
	}

	privring, err := openpgp.ReadKeyRing(privkeyfile)
	if err != nil {
		fmt.Println("Failed to open secring", err)
		return nil, err
	}

	//reader := bytes.NewReader(data)
	//brk,_ := ioutil.ReadAll(data)
	//fmt.Println("wtf",string(brk))
	//fmt.Println("here is where eof panic")

	if len(pass) == 0 {
		fmt.Fprintf(os.Stderr, "Password: ")
		pass, err = terminal.ReadPassword(0)
		fmt.Println("pass",string(pass))
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(os.Stderr, "")
	}

	for _, entity := range privring {
		if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
			entity.PrivateKey.Decrypt(pass)
		}

		for _, subkey := range entity.Subkeys {
			if subkey.PrivateKey != nil && subkey.PrivateKey.Encrypted {
				subkey.PrivateKey.Decrypt(pass)
			}
		}
	}


	if isarmored {
		block, err := armor.Decode(data)
		if err != nil {
			fmt.Println(err)
			panic(err)
		}

		md, err := openpgp.ReadMessage(block.Body, privring, nil, nil)
		if err != nil {
			return nil, err
		}

		return md.UnverifiedBody, nil
	} else {
		md, err := openpgp.ReadMessage(data, privring, nil, nil)
		if err != nil {
			return nil, err
		}

		return md.UnverifiedBody, nil
	}

}
*/

func encryptOpenpgp(data io.Reader, recipient string, gpghome string) (*bytes.Buffer, error) {
	pubkeyfile, err := os.Open(fmt.Sprintf("%s%spubring.gpg", gpghome, string(os.PathSeparator)))
	if err != nil {
		fmt.Println("Failed to open pubring", err)
		return nil, err
	}

	pubring, err := openpgp.ReadKeyRing(pubkeyfile)
	if err != nil {
		fmt.Println("Failed to open pubring", err)
		return nil, err
	}

	pubkey := findKey(pubring, recipient)

	buf := bytes.NewBuffer(nil)
	//w, _ := armor.Encode(buf, "PGP MESSAGE", nil)
	plaintext, err := openpgp.Encrypt(buf, []*openpgp.Entity{pubkey}, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	//reader := bytes.NewReader(data)
	_, err = io.Copy(plaintext, data)
	plaintext.Close()
	//w.Close()
	//buf.Close()
	if err != nil {
		return nil, err
	}

	return buf, nil

}

func saveIndex(index *Index, dspath string) error {
	fh, err := os.OpenFile(dspath+"/ipboh-index.txt", os.O_RDWR, 0600)
	if err != nil {
		fh, err = os.Create(dspath + "/ipboh-index.txt")
		if err != nil {
			return err
		}
	}
	defer fh.Close()

	rawb, err := json.Marshal(index)
	if err != nil {
		return err
	}

	fh.Write(rawb)

	if err != nil {
		return err
	}

	return nil
}

func parseList(n *core.IpfsNode, ctx context.Context, hash string) ([]string, error) {
	list := []string{""}
	fmt.Println("Fetching list")
	reader, err := coreunix.Cat(ctx, n, hash)
	if err != nil {
		return nil, err
	}

	rawbytes, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	list = strings.Split(string(rawbytes), "\n")
	fmt.Println("Parsed list")
	return list, nil

}

func loadIndex(n *core.IpfsNode, ctx context.Context, dspath string) *Index {
	index := makeIndex()

	fh, err := os.Open(dspath + "/ipboh-index.txt")
	if err != nil {
		return index
	}
	defer fh.Close()

	rawb, err := ioutil.ReadAll(fh)
	if err != nil {
		fmt.Println("Failed to read index:", err)
		return index
	}

	err = json.Unmarshal(rawb, index)
	if err != nil {
		fmt.Println("Failed to load index:", err)
		return index
	}

	// if there are readlist and hidelist entries, we want to grab them and parse them
	foundr := false
	foundw := false
	for i := len(index.Entries) - 1; i >= 0; i-- {
		var err error
		if foundr && foundw {
			break
		}

		if !foundr && index.Entries[i].Name == "readlist" {
			fmt.Println("foundreadlist")
			index.ReadList, err = parseList(n, ctx, index.Entries[i].Hash)
			if err != nil {
				fmt.Println("Failed to read readlist.")
			}
			foundr = true
		}

		if !foundw && index.Entries[i].Name == "writelist" {
			fmt.Println("foundwritelist")
			index.WriteList, err = parseList(n, ctx, index.Entries[i].Hash)
			if err != nil {
				fmt.Println("Failed to read writelist.")
			}
			foundw = true
		}
	}

	return index

}

func makeIndex() *Index {
	entries := make([]*Entry, 0)
	return &Index{Entries: entries}
}

type serverContentReader struct {
	r         io.Reader
	namebytes []byte
	n         int
}

func (rdr *serverContentReader) Name() string {
	return strings.TrimSpace(string(rdr.namebytes))
}

func (rdr *serverContentReader) Read(p []byte) (int, error) {
	//fmt.Println("rdr.n", rdr.n)
	headerlength := HEADER_SIZE
	if rdr.n < headerlength {
		rdr.namebytes = make([]byte, headerlength, headerlength)
		blockLen, err := rdr.r.Read(rdr.namebytes)
		if err != nil {
			return blockLen, err
		}
		rdr.n = rdr.n + blockLen
	}

	blockLen, err := rdr.r.Read(p)
	if err != nil {
		return 0, err
	}

	if blockLen == 0 {
		return 0, io.EOF
	}

	rdr.n = rdr.n + blockLen

	return blockLen, nil
}

type clientContentReader struct {
	r    io.Reader
	name string
	n    int
}

func (rdr *clientContentReader) Read(p []byte) (int, error) {

	headerlength := HEADER_SIZE
	if rdr.n < headerlength {
		namebytes := []byte(rdr.name)
		space := []byte(" ")

		var i int
		for i = 0; i <= len(p)-1; i++ {
			if rdr.n >= headerlength {
				return i, nil
			}

			if rdr.n >= len(namebytes) {
				p[i] = space[0]
				rdr.n = rdr.n + 1
				continue
			}

			p[i] = namebytes[rdr.n]
			rdr.n = rdr.n + 1
		}
		return i, nil
	}

	blockLen, err := rdr.r.Read(p)
	if err != nil {
		return 0, err
	}

	if blockLen == 0 {
		return 0, io.EOF
	}

	rdr.n = rdr.n + blockLen

	return blockLen, nil
}

func hasCmd(cmdname string) bool {
	for i := range os.Args {
		if os.Args[i] == cmdname {
			return true
		}
	}
	return false
}

func getCmdArg(cmdname string) string {
	for i := range os.Args {
		if os.Args[i] == cmdname {
			return os.Args[i+1]
		}
	}
	return ""

}

func readIpbohConfig(filepath string) *IpbohConfig {

	ipbohconfig := &IpbohConfig{}

	fh, err := os.Open(filepath)
	defer fh.Close()
	if err != nil {
		return ipbohconfig
	}

	rawb, err := ioutil.ReadAll(fh)
	if err != nil {
		return ipbohconfig
	}
	err = json.Unmarshal(rawb, ipbohconfig)
	if err != nil {
		fmt.Println("Failed to unmarshall:", err)
		return ipbohconfig
	}

	return ipbohconfig

}

func saveIpbohConfig(ipbohconfig *IpbohConfig, filepath string) error {
	//fmt.Println("Saving",ipbohconfig,"to",filepath)
	rawb, err := json.Marshal(ipbohconfig)
	if err != nil {
		fmt.Println("Failed to marshal:", err)
		return err
	}

	os.Remove(filepath)

	fh, err := os.Create(filepath)
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer fh.Close()

	_, err = fh.Write(rawb)
	if err != nil {
		panic(err)
	}

	return nil
}

func getHomeDir() string {
	home := os.Getenv("HOME")
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	}

	return home
}

func getGpghomeDir(home string) string {
	gpgdir := ""
	if runtime.GOOS == "windows" {
		gpgdir = fmt.Sprintf("%s\\gnupg", home)
	} else {
		gpgdir = fmt.Sprintf("%s/.gnupg", home)
	}

	return gpgdir
}

func getUpdateConfig(filepath string, serverhash string, port int) (string, int) {

	ipbohconfig := readIpbohConfig(filepath)

	if ipbohconfig.Port == 0 {
		ipbohconfig.Port = 9898
	}

	if port != 9898 && ipbohconfig.Port != port {
		ipbohconfig.Port = port
		saveIpbohConfig(ipbohconfig, filepath)
	}

	if serverhash != "" && ipbohconfig.Serverhash != serverhash {
		ipbohconfig.Serverhash = serverhash
		saveIpbohConfig(ipbohconfig, filepath)
	}

	return ipbohconfig.Serverhash, ipbohconfig.Port

}

func pin(n *core.IpfsNode, ctx context.Context, hash string) error {

	hashkey := key.B58KeyDecode(hash)
	node, err := n.DAG.Get(ctx, hashkey)
	if err != nil {
		return err
	}

	err = n.Pinning.Pin(ctx, node, false)
	return err

}

func clientHandlerCat(ctx context.Context, w http.ResponseWriter, n *core.IpfsNode, hash, targethash string) {
	target, err := peer.IDB58Decode(targethash)
	if err != nil {
		http.Error(w, fmt.Sprintf("%s", err), 500)
	}

	// FIXME: validate this in case there is a 46 len name!
	foundhash := false
	w.Header().Set("Content-Disposition", fmt.Sprintf("filename=\"%s\"", hash))
	if len(hash) != 46 {
		entrylist := getEntryList(n, target)
		//fmt.Println(entrylist)
		for i := len(entrylist.Entries) - 1; i >= 0; i-- {
			if entrylist.Entries[i].Name == hash {
				hash = entrylist.Entries[i].Hash
				foundhash = true
				break
			}
		}
	} else {
		foundhash = true
	}

	if !foundhash {
		http.Error(w, "No entry found.", 500)
		return
	}

	reader, err := coreunix.Cat(ctx, n, hash)
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(w, reader)
	if err != nil {
		http.Error(w, fmt.Sprintln("Error reading or writing entry:", err), 500)
		return
	}
}

func clientHandlerSync(w http.ResponseWriter, ctx context.Context, n *core.IpfsNode, dspath string, targethash string, reloadindex chan *Entry) {
	target, err := peer.IDB58Decode(targethash)
	if err != nil {
		panic(err)
	}

	curentries := loadIndex(n, ctx, dspath)
	entrymap := make(map[string]bool)
	if len(curentries.Entries) != 0 {
		for i := range curentries.Entries {
			key := fmt.Sprintf("%v", curentries.Entries[i])
			//fmt.Println(key)
			//entrymap[curentries.Entries[i].Hash] = curentries.Entries[i]
			entrymap[key] = true
		}
	}

	fmt.Fprintln(w, "Syncing...", target)
	entrylist := getEntryList(n, target)

	for i := range entrylist.Entries {
		fmt.Fprintln(w, "Downloading ", entrylist.Entries[i].Name)
		reader, err := coreunix.Cat(ctx, n, entrylist.Entries[i].Hash)
		if err != nil {
			panic(err)
		}
		ioutil.ReadAll(reader)

		if len(curentries.Entries) != 0 {
			_, ok := entrymap[fmt.Sprintf("%v", entrylist.Entries[i])]
			if ok {
				fmt.Fprintln(w, "Already have", entrylist.Entries[i].Hash)
			} else {
				fmt.Fprintln(w, "Appending", entrylist.Entries[i].Hash)
				curentries.Entries = append(curentries.Entries, entrylist.Entries[i])
			}
		}


		//FIXME: potential data corruption because it could collide with the main startserver thread running
		err = pin(n, ctx, entrylist.Entries[i].Hash)
		if err != nil {
			panic(err)
		}
		fmt.Fprintln(w, "Pinned", entrylist.Entries[i].Hash, entrylist.Entries[i].Name)
	}

	if len(curentries.Entries) != 0 {
		saveIndex(curentries, dspath)
	} else {
		saveIndex(entrylist, dspath)
	}

	fmt.Fprintln(w, "Sync complete.")
	reloadindex <- &Entry{}
}

func clientHandlerLs(w http.ResponseWriter, n *core.IpfsNode, targethash string) {
	target, err := peer.IDB58Decode(targethash)
	if err != nil {
		http.Error(w, fmt.Sprintf("%s", err), 500)
	}

	entrylist := getEntryList(n, target)
	elbytes, err := json.Marshal(entrylist)
	//fmt.Println("ls request sending ", string(elbytes))
	if err != nil {
		http.Error(w, fmt.Sprintln("Error marshaling json:", err), 500)
		return
	}
	w.Write(elbytes)
}

func clientHandlerAdd(w http.ResponseWriter, rdr io.Reader, n *core.IpfsNode, targethash string) {
	target, err := peer.IDB58Decode(targethash)
	if err != nil {
		http.Error(w, fmt.Sprintf("%s", err), 500)
	}

	//fmt.Println("Dialing...", targethash)
	con, err := MyDial(n, target, "/pack/add")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer con.Close()

	_, err = io.Copy(con, rdr)
	if err != nil {
		http.Error(w, fmt.Sprintln("Error adding file:", err), 500)
		return
	}
}

func clientHandlerIndex(w http.ResponseWriter, ctx context.Context, n *core.IpfsNode, baseurl, targethash string, verbose bool) {
	target, err := peer.IDB58Decode(targethash)
	if err != nil {
		http.Error(w, fmt.Sprintf("%s", err), 500)
	}

	entrylist := getEntryList(n, target)

	if verbose {
		fmt.Fprintln(w, "<html><body><p><a href=\"/\">(default list)</a></p><p><pre><ul style=\"list-style: none;\">")
		for i := len(entrylist.Entries) - 1; i >= 0; i-- {
			ts := entrylist.Entries[i].Timestamp.Format("2006-01-02T15:04")
			fmt.Fprintf(w, "<li><a href=\"/cat?hash=%s&target=%s\">%s</a> %s %s <a href=\"/cat?hash=%s&target=%s\">%s</a></li>", entrylist.Entries[i].Hash, targethash, entrylist.Entries[i].Hash, ts, bytefmt.ByteSize(uint64(entrylist.Entries[i].Size)), entrylist.Entries[i].Name, targethash, entrylist.Entries[i].Name)
		}
		fmt.Fprintln(w, "</ul></pre></p></body></html>")
	} else {

		seen := make(map[string]bool)
		hidelist := getHideList(targethash, baseurl, entrylist)

		fmt.Fprintln(w, "<html><body><p><a href=\"/?verbose=true\">(verbose list)</a></p><p><pre><ul style=\"list-style: none;\">")
		for i := len(entrylist.Entries) - 1; i >= 0; i-- {
			_, exists := seen[entrylist.Entries[i].Name]
			_, existsh := hidelist[entrylist.Entries[i].Name]
			if !exists && !existsh {
				fmt.Fprintf(w, "<li><a href=\"/cat?hash=%s&target=%s\">%s</a> <a href=\"/cat?hash=%s&target=%s\">%s</a></li>", entrylist.Entries[i].Hash, targethash, entrylist.Entries[i].Hash, entrylist.Entries[i].Name, targethash, entrylist.Entries[i].Name)
			}
			seen[entrylist.Entries[i].Name] = true
		}
		fmt.Fprintln(w, "</ul></pre></p></body></html>")
	}
	return
}

func startClientServer(ctx context.Context, n *core.IpfsNode, baseurl string, defsrvhash string, dspath string, timeout time.Duration, reloadindex chan *Entry) {


	amiready := false

	go func() {
		fmt.Fprintln(os.Stderr,"Bootstrapping IPFS...")
		for {
			peers := n.Peerstore.Peers()
			if len(peers) >= REQUIREDPEERS {
				amiready = true
				fmt.Fprintln(os.Stderr, "Peers:",len(peers))
				return
			}
			fmt.Fprintln(os.Stderr, "Peers:",len(peers))


			time.Sleep(1*time.Second)
		}
	}()

	timer := time.NewTimer(timeout)
	if timeout != 0 {
		go func() {
			<-timer.C
			fmt.Println("ipboh clientserver idle timer expired")
			os.Exit(0)
		}()
	}

	http.HandleFunc("/areuthere", func(w http.ResponseWriter, r *http.Request) {
		if !amiready {
			http.Error(w,"Not ready", 400)
			return
		}
		timer.Reset(timeout)
		fmt.Fprintf(w,"%s",n.Identity.Pretty())
		return
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(timeout)
		r.ParseForm()
		var targethash string
		ar, e := r.Form["target"]

		if !e {
			targethash = defsrvhash
		} else {
			targethash = ar[0]
		}
		_, verbose := r.Form["verbose"]

		clientHandlerIndex(w, ctx, n, baseurl, targethash, verbose)

	})

	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(timeout)
		r.ParseForm()
		targethash := r.Form["target"][0]

		clientHandlerAdd(w, r.Body, n, targethash)

	})

	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(timeout)
		r.ParseForm()
		targethash := r.Form["target"][0]

		clientHandlerSync(w, ctx, n, dspath, targethash, reloadindex)

	})

	http.HandleFunc("/ls", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(timeout)
		r.ParseForm()
		targethash := r.Form["target"][0]

		clientHandlerLs(w, n, targethash)

	})

	http.HandleFunc("/cat", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(timeout)
		r.ParseForm()
		hash := r.Form["hash"][0]
		targethash := r.Form["target"][0]

		clientHandlerCat(ctx, w, n, hash, targethash)

	})

	urlp, err := url.Parse(baseurl)
	if err != nil {
		panic(urlp)
	}
	httpd := &http.Server{
		Addr: urlp.Host,
	}
	err = httpd.ListenAndServe()
	if err != nil {
		fmt.Println("Failed to start client server on", baseurl, ":", err)
	}

}

func waitForClientserver(count int, baseurl string) error {
	for i := 0; i <= count; i++ {
		resp, err := http.Get(fmt.Sprintf("%s/areuthere", baseurl))
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode != 200 {
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode == 200 {
			return nil
		}
	}
	return errors.New("Waited too long for clientserver")
}

func hasHideList(entrylist *Index) bool {
	for i := range entrylist.Entries {
		if entrylist.Entries[i].Name == "hidelist" {
			return true
		}
	}
	return false
}

func getHideList(serverhash string, baseurl string, entrylist *Index) map[string]bool {
	entries := make(map[string]bool)
	if !hasHideList(entrylist) {
		return entries
	}

	resp, err := http.Get(fmt.Sprintf("%s/cat?hash=%s&target=%s", baseurl, "hidelist", serverhash))
	if err != nil {
		panic(err)
	}

	entries["hidelist"] = true
	rawbytes, err := ioutil.ReadAll(resp.Body)
	entriesl := strings.Split(string(rawbytes), "\n")
	for i := range entriesl {
		entries[entriesl[i]] = true
	}
	return entries
}

func listEntries(baseurl string, serverhash string, verbose bool) {
	resp, err := http.Get(fmt.Sprintf("%s/ls?target=%s", baseurl, serverhash))
	if err != nil {
		panic(err)
	}

	entrylist := &Index{}
	rawbytes, err := ioutil.ReadAll(resp.Body)
	//fmt.Println("got raw bytes", string(rawbytes))
	if err != nil {
		fmt.Println("Error reading response from localhost")
		panic(err)
	}
	err = json.Unmarshal(rawbytes, entrylist)
	if err != nil {
		fmt.Println("Failed to unmarshal:", err)
	}

	seen := make(map[string]bool)
	hidelist := getHideList(serverhash, baseurl, entrylist)

	// reverse the list
	for i := len(entrylist.Entries) - 1; i >= 0; i-- {

		if verbose {
			//ts := entrylist.Entries[i].Timestamp.Format(time.RFC3339)
			ts := entrylist.Entries[i].Timestamp.Format("2006-01-02T15:04")
			fmt.Println(entrylist.Entries[i].Hash, ts, bytefmt.ByteSize(uint64(entrylist.Entries[i].Size)), entrylist.Entries[i].Name)
			continue
		}

		_, exists := seen[entrylist.Entries[i].Name]
		_, existsh := hidelist[entrylist.Entries[i].Name]
		if !exists && !existsh {
			fmt.Println(entrylist.Entries[i].Hash, entrylist.Entries[i].Name)
		}
		seen[entrylist.Entries[i].Name] = true

	}

}

func syncRemote(baseurl string, serverhash string) (rdr io.Reader) {
	resp, err := http.Get(fmt.Sprintf("%s/sync?target=%s", baseurl, serverhash))
	if err != nil {
		panic(err)
	}

	return resp.Body
}

func catContent(catarg string, baseurl string, serverhash string) (rdr io.Reader) {

	resp, err := http.Get(fmt.Sprintf("%s/cat?hash=%s&target=%s", baseurl, catarg, serverhash))
	if err != nil {
		panic(err)
	}

	return resp.Body

}

func catCatContent(resp io.Reader, wtr io.Writer) {

	//defer resp.Close()
	//bufr := bufio.NewReader(resp)
	//if err != nil && fmt.Sprintf("%s", err) != "EOF" {
	//	panic(err)
	//}

	_, err := io.Copy(wtr, resp)
	if err != nil {
		fmt.Fprintln(os.Stderr,"Failed to write to stdout")
		panic(err)
	}
}

func addContent(add string, gpghome string, recipient string, baseurl string, serverhash string) {

	var encbuf *bytes.Buffer
	var newcontent *clientContentReader
	var err error
	if recipient != "" {
		encbuf, err = encryptOpenpgp(os.Stdin, recipient, gpghome)
		if err != nil {
			panic("Failed to encrypt.")
		}
	}

	sbytes := []byte(add)
	if len(sbytes) > HEADER_SIZE {
		panic(fmt.Sprintf("Name '%s' longer than %d", sbytes, HEADER_SIZE))
	}

	if encbuf != nil {
		newcontent = &clientContentReader{name: add, r: encbuf}
		//io.Copy(encbuf,newcontent.r)
		//newcontent.r = encbuf
	} else {
		newcontent = &clientContentReader{name: add, r: os.Stdin}
	}
	resp, err := http.Post(fmt.Sprintf("%s/add?target=%s", baseurl, serverhash), "application/json", newcontent)
	resp.Body.Close()

	if err != nil {
		panic(err)
	}
}

func startupIPFS(dspath string, ctx *context.Context) (*core.IpfsNode, error) {
	r, err := fsrepo.Open(dspath)
	if err != nil {
		config, err := config.Init(os.Stdout, 2048)
		if err != nil {
			return nil, err
		}

		if err := fsrepo.Init(dspath, config); err != nil {
			return nil, err
		}

		r, err = fsrepo.Open(dspath)
		if err != nil {
			return nil, err
		}
	}

	//fmt.Println(r)
	node, err := core.NewNode(*ctx, &core.BuildCfg{Online: true, Repo: r})
	if err != nil {
		return nil, err
	}

	return node, nil

}

func basicInit() (string, string, string) {

	home := getHomeDir()
	gpghomeDefault := getGpghomeDir(home)

	dspath := ""
	if runtime.GOOS == "windows" {
		dspath = fmt.Sprintf("%s\\ipfsrepo", home)
	} else {
		dspath = fmt.Sprintf("%s/.ipfs", home)
	}

	return dspath, home, gpghomeDefault
}

func parseCommandFromArgs() (bool, bool, bool, string, string) {
	var server, syncremote,id bool
	var add, catarg string
	server = hasCmd("server")
	syncremote = hasCmd("sync")
	id = hasCmd("id")
	if hasCmd("add") {
		add = getCmdArg("add")
	}

	if hasCmd("cat") {
		catarg = getCmdArg("cat")
	}

	return server, syncremote, id, add, catarg
}

// setup initial things, spawn server if needed, any prereqs
func phase1Setup(ctx context.Context, server, spawnClientserver, clientserver bool, dspath string, home, serverhash string, port int) (*core.IpfsNode, string, int, string, bool) {

	var n *core.IpfsNode
	var err error

	// grab or update configs
	filepath := fmt.Sprintf("%s%s.ipbohrc", home, string(os.PathSeparator))
	serverhash, port = getUpdateConfig(filepath, serverhash, port)
	csBaseUrl := fmt.Sprintf("http://localhost:%d", port)

	if server {
		n, err = startupIPFS(dspath, &ctx)
		if err != nil {
			panic(err)
		}

		for _, addr := range n.PeerHost.Addrs() {
			fmt.Printf("Swarm listening on %s\n", addr.String())
		}
	} else if clientserver {
		n, err = startupIPFS(dspath, &ctx)
		if err != nil {
			panic(err)
		}
	} else {
		resp, err := http.Get(fmt.Sprintf("%s/areuthere", csBaseUrl))
		if err != nil {
			spawnClientserver = true
		} else if resp.StatusCode == 200 {
			spawnClientserver = false
		} else {
			spawnClientserver = true
		}

	}

	// spawn a separate process to launch client server if it is not already running
	if spawnClientserver {
		var exePath string
		exePath, err := godaemon.GetExecutablePath()
		if err != nil {
			err = fmt.Errorf("failed to get pid: %v", err)
		}

		files := make([]*os.File, 3, 3)
		files[0], files[1], files[2] = os.Stdin, os.Stdout, os.Stderr
		attrs := os.ProcAttr{Dir: ".", Env: os.Environ(), Files: files}
		_, err = os.StartProcess(exePath, []string{exePath, "-c", "-p", fmt.Sprintf("%d", port)}, &attrs)
		if err != nil {
			panic(err)
		}
	}

	return n, serverhash, port, csBaseUrl, spawnClientserver
}

func startServer(ctx context.Context, n *core.IpfsNode, dspath string, baseurl string, reloadindex chan *Entry) {
	index := loadIndex(n, ctx, dspath)
	mtx := sync.Mutex{}

	getCurIndex := func() *Index {
		mtx.Lock()
		defer mtx.Unlock()
		return index
	}

	go func() {
		prevlen := len(index.Entries)
		for {
			time.Sleep(5*time.Second)
			mtx.Lock()
			if len(index.Entries) > prevlen {
				prevlen = len(index.Entries)
				fmt.Println("Saving index...")
				err := saveIndex(index, dspath)
				if err != nil {
					panic(err)
				}
				//index = loadIndex(n, ctx, dspath)

			}
			mtx.Unlock()
		}
	}()

	go handleIndex(n, ctx, index, getCurIndex)
	go handleAdd(n, ctx, index, &mtx, dspath, reloadindex, getCurIndex)

	go func() {
		for {
			entry := <-reloadindex
			fmt.Println("Need to reload index!")

			mtx.Lock()
			fmt.Println("locky")
			// we just synced and the whole index changed underneath us
			if entry.Name == "" && entry.Hash == "" {
				index = loadIndex(n, ctx, dspath)
			} else {
				index.Entries = append(index.Entries, entry)
			}

			// we don't need to do this everytime...
			/*err := saveIndex(index, dspath)
			if err != nil {
				panic(err)
			}*/

			if entry.Name == "readlist" || entry.Name == "writelist" {
				err := saveIndex(index, dspath)
				if err != nil {
					panic(err)
				}
				index = loadIndex(n, ctx, dspath)
			}
			fmt.Println("unlocky")
			mtx.Unlock()

		}
	}()

	startClientServer(ctx, n, baseurl, "", dspath, 0, reloadindex)
	//wg.Wait()
}

func processClientCommands(spawnClientserver bool, serverhash string, syncremote bool, id bool, add, catarg, gpghome, recipient string, verbose bool, csBaseUrl string) {
	if spawnClientserver {
		//fmt.Println("Sleeping for 10 seconds...\n")
		err := waitForClientserver(120, csBaseUrl)
		if err != nil {
			panic(err)
		}
		//time.Sleep(10 * time.Second)
	}

	if serverhash == "" {
		fmt.Println("Need to specify a remote server node id e.g. -h QmarTZGZDhBpDY5wgx9qSJrFcNokF37iD44Vk2FTYGPyBs")
		return
	}

	// add something
	if add != "" {
		addContent(add, gpghome, recipient, csBaseUrl, serverhash)

		// cat something
	} else if catarg != "" {
		rdr := catContent(catarg, csBaseUrl, serverhash)
		catCatContent(rdr, os.Stdout)
	} else if syncremote {
		rdr := syncRemote(csBaseUrl, serverhash)
		io.Copy(os.Stdout, rdr)
		// fetch entry list by default
	} else if id {
		resp, err := http.Get(fmt.Sprintf("%s/areuthere", csBaseUrl))
		if err != nil {
			panic(err)
		}

		remotehash,err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		fmt.Println("Local",string(remotehash))
		fmt.Println("Remote",serverhash)
	} else {
		listEntries(csBaseUrl, serverhash, verbose)

	}
}

func main() {

	dspath, home, gpghomeDefault := basicInit()

	var server, verbose, clientserver, spawnClientserver, syncremote, id, noenc bool
	var serverhash, add, gpghome  string
	var catarg, recipient string
	var port, timeout int

	flag.BoolVar(&verbose, "v", false, "Verbose")
	flag.StringVar(&recipient, "e", "", "Encrypt to PGP recipient")
	flag.StringVar(&gpghome, "g", gpghomeDefault, "GPG homedir.")
	flag.StringVar(&serverhash, "h", "", "Server hash to connect to")
	flag.StringVar(&dspath, "d", dspath, "Default data path.")
	flag.IntVar(&port, "p", 9898, "Port used by localhost client server (9898)")
	flag.BoolVar(&clientserver, "c", false, "Start client server")
	flag.IntVar(&timeout, "t", 60, "Timeout of server if not used")
	flag.BoolVar(&noenc, "n", false, "Force no encryption if autoenc is set.")
	flag.Parse()

	server, syncremote, id, add, catarg = parseCommandFromArgs()

	reloadindex := make(chan *Entry)

	// 'phase 1' initial setup...
	//ctx, cancel := context.WithCancel(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n, serverhash, port, csBaseUrl, spawnClientserver := phase1Setup(ctx, server, spawnClientserver, clientserver, dspath, home, serverhash, port)

	// 'phase 2' do the things
	// startup the server if that is what we are doing
	if server {
		startServer(ctx, n, dspath, csBaseUrl, reloadindex)
		// start client server
	} else if clientserver {
		go func() {
			for {
				// we don't care about actually reloading anything in client context so just make sure the channel clears
				<-reloadindex
			}
		}()

		startClientServer(ctx, n, csBaseUrl, serverhash, dspath, time.Duration(timeout)*time.Minute, reloadindex)
		// run client command
	} else {

		// Check if we are auto encrypting, prefer -e flag
		filepath := fmt.Sprintf("%s%s.ipbohrc", home, string(os.PathSeparator))
		ipbohconfig := readIpbohConfig(filepath)
		if add != "" && ipbohconfig.Autoenc != "" && recipient == "" {
			recipient = ipbohconfig.Autoenc
		}

		if add != "" && ipbohconfig.Autoenc != "" && noenc == true {
			recipient = ""
		}

		//fmt.Println("Recipient:", recipient)
		processClientCommands(spawnClientserver, serverhash, syncremote, id, add, catarg, gpghome, recipient, verbose, csBaseUrl)
	}

}
