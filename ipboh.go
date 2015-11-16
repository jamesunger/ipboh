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
	"runtime"
	"github.com/pivotal-golang/bytefmt"
	core "github.com/ipfs/go-ipfs/core"
	corenet "github.com/ipfs/go-ipfs/core/corenet"
	coreunix "github.com/ipfs/go-ipfs/core/coreunix"
	importer "github.com/ipfs/go-ipfs/importer"
	"github.com/ipfs/go-ipfs/importer/chunk"
	peer "github.com/ipfs/go-ipfs/p2p/peer"
	pin "github.com/ipfs/go-ipfs/pin"
	fsrepo "github.com/ipfs/go-ipfs/repo/fsrepo"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/ssh/terminal"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	//"syscall"
	"time"
	//"github.com/ipfs/go-ipfs/blocks"
	dag "github.com/ipfs/go-ipfs/merkledag"

	//ds "github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/jbenet/go-datastore"
	"github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/jbenet/go-datastore/flatfs"
	syncds "github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/jbenet/go-datastore/sync"
	bstore "github.com/ipfs/go-ipfs/blocks/blockstore"
	bserv "github.com/ipfs/go-ipfs/blockservice"
	offline "github.com/ipfs/go-ipfs/exchange/offline"

	ft "github.com/ipfs/go-ipfs/unixfs"

	"code.google.com/p/go.net/context"
	dagutils "github.com/ipfs/go-ipfs/merkledag/utils"
	config "github.com/ipfs/go-ipfs/repo/config"
	"github.com/VividCortex/godaemon"
)

type IpbohConfig struct {
	Serverhash string
	Recipient  string
}

type Index struct {
	Entries []*Entry
}

type Entry struct {
	Name string
	Hash string
	Timestamp time.Time
	Size int
}

func handleIndex(n *core.IpfsNode, ctx context.Context, index *Index, wg *sync.WaitGroup) {
	list, err := corenet.Listen(n, "/pack/index")
	if err != nil {
		panic(err)
	}
	fmt.Printf("I have an index: %s\n", n.Identity)

	for {
		con, err := list.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer con.Close()

		fmt.Printf("Connection from: %s\n", con.Conn().RemotePeer())
		indexbytes, err := json.Marshal(index)
		if err != nil {
			panic(err)
		}
		count, err := con.Write(indexbytes)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("Wrote bytes:", count)
		con.Close()
	}

	wg.Done()

}

func NewFlatfsDagService(dspath string) dag.DAGService {
	datastore, err := flatfs.New(dspath, 2)
	if err != nil {
		panic(err)
	}

	bs := bstore.NewBlockstore(syncds.MutexWrap(datastore))
	bsrv := bserv.New(bs, offline.Exchange(bs))
	return dag.NewDAGService(bsrv)
}

//var rootnode *dag.Node
func newDirNode() *dag.Node {
	return &dag.Node{Data: ft.FolderPBData()}
	/*if rootnode == nil {
		return &dag.Node{Data: ft.FolderPBData()}
	} else {
		return rootnode
	}*/
}

func handleAdd(n *core.IpfsNode, ctx context.Context, index *Index, mtx *sync.Mutex, wg *sync.WaitGroup, dspath string) {
	list, err := corenet.Listen(n, "/pack/add")
	if err != nil {
		panic(err)
	}
	//fmt.Printf("I am ready to add: %s\n", n.Identity)

	for {
		fmt.Println("Waiting for add...\n")
		con, err := list.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer con.Close()

		fmt.Printf("Connection from: %s\n", con.Conn().RemotePeer())

		newdirnode := newDirNode()
		e := dagutils.NewDagEditor(NewFlatfsDagService(dspath), newdirnode)



		// wrap the connection in the serverContentReader so we can get our
		// header
		serverReader := &serverContentReader{ r: con }
		chnk, err := chunk.FromString(serverReader, "rabin")
		if err != nil {
			panic(err)
		}

		dagnode, err := importer.BuildDagFromReader(
			n.DAG,
			chnk,
			importer.PinIndirectCB(n.Pinning.GetManual()),
		)

		fmt.Println("They are adding:", serverReader.Name())
		err = e.InsertNodeAtPath(ctx, serverReader.Name(), dagnode, newDirNode)
		if err != nil {
			panic(err)
		}

		err = e.WriteOutputTo(n.DAG)
		if err != nil {
			panic(err)
		}

		key, err := dagnode.Key()
		if err != nil {
			panic(err)
		}
		fmt.Println("Added:", key.B58String())
		entry := Entry{Timestamp: time.Now(), Size: serverReader.n-120, Name: serverReader.Name(), Hash: key.B58String()}

		mtx.Lock()
		index.Entries = append(index.Entries, &entry)
		mtx.Unlock()

		err = saveIndex(index, dspath)
		if err != nil {
			panic(err)
		}

		rootnd := e.GetNode()

		rnk, err := rootnd.Key()
		if err != nil {
			panic(err)
		}
		mp := n.Pinning.GetManual()
		mp.RemovePinWithMode(rnk, pin.Indirect)
		mp.PinWithMode(rnk, pin.Recursive)
		n.Pinning.Flush()

		fmt.Println("Pinned rn:", rnk)

	}

	wg.Done()

}

func getEntryList(n *core.IpfsNode, target peer.ID) *Index {

	index := makeIndex()
	con, err := corenet.Dial(n, target, "/pack/index")
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

func decryptOpenpgp(data []byte, gpghome string, pass []byte) ([]byte, error) {
	privkeyfile, err := os.Open(fmt.Sprintf("%s%ssecring.gpg",gpghome, string(os.PathSeparator)))
	if err != nil {
		fmt.Println("Failed to open secring", err)
		return nil, err
	}

	privring, err := openpgp.ReadKeyRing(privkeyfile)
	if err != nil {
		fmt.Println("Failed to open secring", err)
		return nil, err
	}

	reader := bytes.NewReader(data)
	block, err := armor.Decode(reader)
	if err != nil {
		panic(err)
	}

	if len(pass) == 0 {
		fmt.Fprintf(os.Stderr, "Password: ")
		pass, err = terminal.ReadPassword(0)
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

	md, err := openpgp.ReadMessage(block.Body, privring, nil, nil)
	if err != nil {
		return nil, err
	}

	plaintext, err := ioutil.ReadAll(md.UnverifiedBody)
	if err != nil {
		panic(err)
	}
	return plaintext, nil
}

func encryptOpenpgp(data io.Reader, recipient string, gpghome string) ([]byte, error) {
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
	w, _ := armor.Encode(buf, "PGP MESSAGE", nil)
	plaintext, err := openpgp.Encrypt(w, []*openpgp.Entity{pubkey}, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	//reader := bytes.NewReader(data)
	_, err = io.Copy(plaintext, data)
	plaintext.Close()
	w.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil

}

func saveIndex(index *Index, dspath string) error {

	fh, err := os.OpenFile(dspath+"/ipboh-index.txt", os.O_RDWR, 0600)
	if err != nil {
		fh, err = os.Create(dspath + "/ipboh-index.txt")
		if err != nil {
			return err
		}
	}

	rawb, err := json.Marshal(index)
	if err != nil {
		return err
	}

	fh.Write(rawb)
	fh.Close()

	return nil
}

func loadIndex(dspath string) *Index {
	index := makeIndex()

	fh, err := os.Open(dspath + "/ipboh-index.txt")
	if err != nil {
		return index
	}

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

	return index

}

func makeIndex() *Index {
	entries := make([]*Entry, 0)
	return &Index{Entries: entries}
}

type serverContentReader struct {
	r io.Reader
	namebytes []byte
	n int
}

func (rdr *serverContentReader) Name() string {
	return strings.TrimSpace(string(rdr.namebytes))
}

func (rdr *serverContentReader) Read(p []byte) (int, error) {
	fmt.Println("rdr.n",rdr.n)
	headerlength := 120
	if rdr.n < headerlength {
		rdr.namebytes = make([]byte,120,120)
		blockLen, err := rdr.r.Read(rdr.namebytes)
		if err != nil {
			return blockLen,err
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
	r io.Reader
	name string
	n int
}

func (rdr *clientContentReader) Read(p []byte) (int, error) {


	headerlength := 120
	if rdr.n < headerlength {
		namebytes := []byte(rdr.name)
		space := []byte(" ")

		var i int
		for i = 0; i <= len(p)-1; i++ {
			if rdr.n >= headerlength {
				return i,nil
			}

			if rdr.n >= len(namebytes) {
				p[i] = space[0]
				rdr.n = rdr.n + 1
				continue
			}


			p[i] = namebytes[rdr.n]
			rdr.n = rdr.n + 1
		}
		return i,nil
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

func saveIpohConfig(ipbohconfig *IpbohConfig, filepath string) error {
	//fmt.Println("Saving",ipbohconfig,"to",filepath)
	rawb, err := json.Marshal(ipbohconfig)
	if err != nil {
		fmt.Println("Failed to marshal:", err)
		return err
	}

	fh, err := os.OpenFile(filepath, os.O_RDWR, 0600)
	if err != nil {
		fh, err = os.Create(filepath)
		if err != nil {
			fmt.Println(err)
			return err
		}
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




func getUpdateConfig(conft string, item string) string {

	home := getHomeDir()

	filepath := fmt.Sprintf("%s%s.ipbohrc", home, string(os.PathSeparator))
	ipbohconfig := readIpbohConfig(filepath)

	if item == "" && conft == "Recipient" {
		return ipbohconfig.Recipient
	}

	if item == "" && conft == "Serverhash" {
		return ipbohconfig.Serverhash
	}

	if conft == "Recipient" && ipbohconfig.Recipient != item {
		//fmt.Println("Saving recipient.")
		ipbohconfig.Recipient = item
		saveIpohConfig(ipbohconfig, filepath)
		return item
	}

	if conft == "Serverhash" && ipbohconfig.Serverhash != item {
		//fmt.Println("Saving serverhash:",item)
		ipbohconfig.Serverhash = item
		saveIpohConfig(ipbohconfig, filepath)
		return item
	}

	return item

}

func startClientServer(ctx context.Context, n *core.IpfsNode, port int) {


	//resettime := 60*time.Second
	resettime := 1800*time.Second
	timer := time.NewTimer(resettime) // half hour
	go func() {
		<-timer.C
		//fmt.Println("Timer expired")
		os.Exit(0)
	}()

	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(resettime)
		r.ParseForm()
		targethash := r.Form["target"][0]
		target, err := peer.IDB58Decode(targethash)
		if err != nil {
			http.Error(w,fmt.Sprintf("%s",err),500)
		}

		con, err := corenet.Dial(n, target, "/pack/add")
		if err != nil {
			fmt.Println(err)
			return
		}
		defer con.Close()

		_, err = io.Copy(con, r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error adding file:", err), 500)
			return
		}

	})

	http.HandleFunc("/ls", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(resettime)
		r.ParseForm()
		targethash := r.Form["target"][0]
		target, err := peer.IDB58Decode(targethash)
		if err != nil {
			http.Error(w,fmt.Sprintf("%s",err),500)
		}

		entrylist := getEntryList(n, target)
		elbytes, err := json.Marshal(entrylist)
		//fmt.Println("ls request sending ", string(elbytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("Error marshaling json:", err), 500)
			return
		}
		w.Write(elbytes)
	})

	http.HandleFunc("/cat", func(w http.ResponseWriter, r *http.Request) {
		timer.Reset(resettime)
		r.ParseForm()
		hash := r.Form["hash"][0]
		targethash := r.Form["target"][0]
		target, err := peer.IDB58Decode(targethash)
		if err != nil {
			http.Error(w,fmt.Sprintf("%s",err),500)
		}

		// FIXME: validate this in case there is a 46 len name!
		foundhash := false
		if len(hash) != 46 {
			entrylist := getEntryList(n, target)
			//fmt.Println(entrylist)
			for i := len(entrylist.Entries)-1; i >= 0; i-- {
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
			http.Error(w, fmt.Sprintf("Error reading or writing entry:", err), 500)
			return
		}

	})

	httpd := &http.Server{
		Addr: fmt.Sprintf("%s:%d", "127.0.0.1", port),
	}
	httpd.ListenAndServe()
}

func waitForClientserver(count int) error {
	for i := 0; i <= count; i++ {
		resp, err := http.Get("http://localhost:9898/")
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode == 404 {
			return nil
		}
	}
	return errors.New("Waited too long for clientserver")
}

func main() {

	var wg sync.WaitGroup
	wg.Add(2)

	index := makeIndex()
	home := getHomeDir()
	gpghomeDefault := getGpghomeDir(home)

	var server, verbose, clientserver, spawnClientserver bool
	var serverhash, add, dspath,gpghome,gpgpass string
	var catarg, recipient string
	var port int
	flag.BoolVar(&verbose, "v", false, "Verbose")
	flag.StringVar(&recipient, "e", "", "Encrypt or decrypt to PGP recipient")
	//flag.StringVar(&dspath, "d", "/tmp/ipboh-data", "Data store path, by default /tmp/ipboh-data")
	flag.StringVar(&gpghome, "g", gpghomeDefault, "GPG homedir.")
	flag.StringVar(&gpgpass, "gpass", "", "GPG password. This is insecure and only used on Windows where reading from the terminal breaks.")
	flag.StringVar(&serverhash, "h", "", "Server hash to connect to")
	flag.IntVar(&port, "p", 9898, "Port used by localhost client server (9898)")
	flag.BoolVar(&clientserver, "c", false, "Start client server")
	flag.Parse()


	if runtime.GOOS == "windows" {
		dspath = fmt.Sprintf("%s\\ipfsrepo",home)
	} else {
		dspath = fmt.Sprintf("%s/.ipfs",home)
	}


	server = hasCmd("server")
	if hasCmd("add") {
		add = getCmdArg("add")
	}

	if hasCmd("cat") {
		catarg = getCmdArg("cat")
	}

	serverhash = getUpdateConfig("Serverhash", serverhash)

	var ctx context.Context
	var n *core.IpfsNode

	if server || clientserver {
		r, err := fsrepo.Open(dspath)
		//if err != nil && strings.Contains(fmt.Sprintf("%s",err),"temporar")
		if err != nil {
			config, err := config.Init(os.Stdout, 2048)
			if err != nil {
				panic(err)
			}


			if err := fsrepo.Init(dspath, config); err != nil {
				panic(err)
			}

			r, err = fsrepo.Open(dspath)
			if err != nil {
				panic(err)
			}
		}

		cotx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctx = cotx

		node, err := core.NewNode(ctx, &core.BuildCfg{Online: true, Repo: r})
		if err != nil {
			panic(err)
		}
		n = node
		fmt.Println("initialized..")
	} else {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err != nil {
			//fmt.Println("Need to spawn..", clientserver,os.Args);
			spawnClientserver = true
		} else if resp.StatusCode == 404 {
			//fmt.Println("No need to spawn..");
			spawnClientserver = false
		} else {
			//fmt.Println("Need to spawn..", resp);
			spawnClientserver = true
		}

	}

	if spawnClientserver {
		// FIXME to be portable
		var exePath string
		exePath,err := godaemon.GetExecutablePath()
		if err != nil {
			err = fmt.Errorf("failed to get pid: %v", err)
		}

		files := make([]*os.File, 3, 3)
		files[0], files[1], files[2] = os.Stdin, os.Stdout, os.Stderr
		attrs := os.ProcAttr{Dir: ".", Env: os.Environ(), Files: files}
		_, err = os.StartProcess(exePath, []string{exePath, "-c"}, &attrs)
		if err != nil {
			panic(err)
		}
	}

	// startup the server if that is what we are doing
	if server {


		index = loadIndex(dspath)
		mtx := sync.Mutex{}

		go handleIndex(n, ctx, index, &wg)
		go handleAdd(n, ctx, index, &mtx, &wg, dspath)
		wg.Wait()

		// make sure we have a serverhash, we'll need it for client or clientserver
	} else if serverhash == "" {
		fmt.Println("Need to specify a remote server node id e.g. -h QmarTZGZDhBpDY5wgx9qSJrFcNokF37iD44Vk2FTYGPyBs")
		return

		// start client server
	} else if clientserver {

		/*if len(n.Peerstore.Addrs(target)) == 0 {
			if verbose {
				fmt.Println("Looking for peer: ", target.Pretty())
			}
			ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
			defer cancel()
			p, err := n.Routing.FindPeer(ctx, target)
			if err != nil {
				fmt.Println("Failed to find peer: ", err)
				return
			}
			if verbose {
				fmt.Println("Found peer: ", p.Addrs)
			}
			n.Peerstore.AddAddrs(p.ID, p.Addrs, peer.TempAddrTTL)
		}*/

		wg.Add(1)
		startClientServer(ctx, n, port)

		// run client command
	} else {
		if spawnClientserver {
			//fmt.Println("Sleeping for 10 seconds...\n")
			err := waitForClientserver(20)
			if err != nil {
				panic(err)
			}
		}

		// add something
		if add != "" {

			///newcontent := getNewContent(add)

			var encbytes []byte
			var err error
			if recipient != "" {
				encbytes, err = encryptOpenpgp(os.Stdin, recipient, gpghome)
				if err != nil {
					fmt.Println("Failed to encrypt.")
				}
			}

			//contentbytes, err := json.Marshal(newcontent)

			// construct reader that spits out: name\nDATA... stream
			//newcontentReader := getNewContent()
			newcontent := &clientContentReader{ name: add, r: os.Stdin }
			if len(encbytes) != 0 {
				buf := bytes.NewBuffer(encbytes)
				newcontent.r = buf
			}
			resp, err := http.Post(fmt.Sprintf("http://localhost:%d/add?target=%s", port, serverhash), "application/json", newcontent)

			if err != nil {
				panic(err)
			}
			defer resp.Body.Close()

			// cat something
		} else if catarg != "" {

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/cat?hash=%s&target=%s", port, catarg, serverhash))
			if err != nil {
				panic(err)
			}

			bytes, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			}

			ispgp := false
			if len(bytes) >= 40 {
				initialbytes := bytes[0:40]
				if strings.Contains(string(initialbytes), "BEGIN PGP MESSAGE") {
					ispgp = true
				}
			}

			if ispgp {
				//fmt.Println("orig", string(bytes))
				bytes, err = decryptOpenpgp(bytes,gpghome,[]byte(gpgpass))
				if err != nil {
					fmt.Println("Failed to decrypt:", err)
					return
				}
			}

			os.Stdout.Write(bytes)

			// fetch entry list by default
		} else {

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/ls?target=%s", port, serverhash))
			if err != nil {
				panic(err)
			}

			entrylist := &Index{}
			rawbytes, err := ioutil.ReadAll(resp.Body)
			//fmt.Println("got raw bytes", string(rawbytes))
			if err != nil {
				fmt.Println("Error reading response from localhost\n")
				panic(err)
			}
			err = json.Unmarshal(rawbytes, entrylist)
			if err != nil {
				fmt.Println("Failed to unmarshal:", err)
			}

			seen := make(map[string]bool)
			// reverse the list
			for i := len(entrylist.Entries)-1; i >= 0; i-- {

				if verbose {
					//ts := entrylist.Entries[i].Timestamp.Format(time.RFC3339)
					ts := entrylist.Entries[i].Timestamp.Format("2006-01-02T15:04")
					fmt.Println(entrylist.Entries[i].Hash, ts, bytefmt.ByteSize(uint64(entrylist.Entries[i].Size)),entrylist.Entries[i].Name)
					continue
				}

				_, exists := seen[entrylist.Entries[i].Name];
				if !exists {
					fmt.Println(entrylist.Entries[i].Hash, entrylist.Entries[i].Name)
				}
				seen[entrylist.Entries[i].Name] = true
			}

		}
	}

}
