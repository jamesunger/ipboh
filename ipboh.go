package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	"syscall"
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
}

type Add struct {
	Name    string
	Content []byte
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

func NewMemoryDagService(dspath string) dag.DAGService {
	// build mem-datastore for editor's intermediary nodes
	datastore, err := flatfs.New(dspath, 2)
	if err != nil {
		panic(err)
	}
	//bs := bstore.NewBlockstore(syncds.MutexWrap(ds.NewMapDatastore()))
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

func handleAdd(n *core.IpfsNode, ctx context.Context, index *Index, wg *sync.WaitGroup, dspath string) {
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
		readbytes, err := ioutil.ReadAll(con)
		// should not die here
		if err != nil {
			fmt.Println("Failed to ready all bytes: ", err)
			continue
		}

		newadd := &Add{}
		err = json.Unmarshal(readbytes, newadd)
		if err != nil {
			fmt.Println("Failed to unmarshal json: ", err)
			continue
		}

		fmt.Println("They are adding:", newadd.Name)

		/*
			b := blocks.NewBlock(newadd.Content)
			fmt.Println("Block:", b.Key().B58String())

			blockadd,err := n.Blocks.AddBlock(b)
			if err != nil {
				panic(err)
			}
			fmt.Println(blockadd)
		*/

		//dagnode := &dag.Node{Data: newadd.Content}
		//dagadd, err := n.DAG.Add(dagnode)
		//if err != nil {
		//		panic(err)
		//       }

		newdirnode := newDirNode()
		e := dagutils.NewDagEditor(NewMemoryDagService(dspath), newdirnode)

		reader := bytes.NewReader(newadd.Content)
		chnk, err := chunk.FromString(reader, "rabin")
		if err != nil {
			panic(err)
		}

		dagnode, err := importer.BuildDagFromReader(
			n.DAG,
			chnk,
			importer.PinIndirectCB(n.Pinning.GetManual()),
		)

		err = e.InsertNodeAtPath(ctx, newadd.Name, dagnode, newDirNode)
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
		entry := Entry{Name: newadd.Name, Hash: key.B58String()}
		index.Entries = append(index.Entries, &entry)

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

	//	rawbytes := make([]byte, 2048, 2048)
	//	buf := bytes.NewBuffer(rawbytes)
	//	_,err = io.Copy(buf, con)

	rawbytes, err := ioutil.ReadAll(con)
	if err != nil {
		fmt.Println(err)
		return index
	}
	//fmt.Println("Got result:" + string(rawbytes))

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

func decryptOpenpgp(data []byte) ([]byte, error) {
	home := os.Getenv("HOME")
	privkeyfile, err := os.Open(fmt.Sprintf("%s/.gnupg/secring.gpg", home))
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

	fmt.Fprintf(os.Stderr, "Password: ")
	passphrase, err := terminal.ReadPassword(0)
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, "")

	for _, entity := range privring {
		//for _, ident := range entity.Identities {
		if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
			//fmt.Println("Decrypting private key using passphrase")
			entity.PrivateKey.Decrypt([]byte(passphrase))
			//if err != nil  && verbose {
			//	fmt.Println("Failed to decrypt key")
			//}
		}

		for _, subkey := range entity.Subkeys {
			if subkey.PrivateKey != nil && subkey.PrivateKey.Encrypted {
				subkey.PrivateKey.Decrypt([]byte(passphrase))
				//if err != nil && verbose {
				//	fmt.Println("Failed to decrypt subkey")
				//}
			}
		}
		//if strings.Contains(ident.Name, name) {
		//	return entity
		//}
		//}
	}

	//privkey := findKey(privring, recipient)
	//if privkey == nil {
	//	return nil, errors.New("Associated private key not found.")
	//}

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

func encryptOpenpgp(data []byte, recipient string) ([]byte, error) {
	home := os.Getenv("HOME")
	pubkeyfile, err := os.Open(fmt.Sprintf("%s/.gnupg/pubring.gpg", home))
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
	reader := bytes.NewReader(data)
	_, err = io.Copy(plaintext, reader)
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

func getNewContent(name string) Add {

	rawbytes, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}

	return Add{Name: name, Content: rawbytes}

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

func getUpdateConfig(conft string, item string) string {

	filepath := fmt.Sprintf("%s/.ipbohrc", os.Getenv("HOME"))
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
	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
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
			for i := range entrylist.Entries {
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

// ripped from https://github.com/VividCortex/godaemon/blob/master/os.go
func Readlink(name string) (string, error) {
	for len := 128; ; len *= 2 {
		b := make([]byte, len)
		n, e := syscall.Readlink(name, b)
		if e != nil {
			return "", &os.PathError{"readlink", name, e}
		}
		if n < len {
			if z := bytes.IndexByte(b[:n], 0); z >= 0 {
				n = z
			}
			return string(b[:n]), nil
		}
	}
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

	var server, verbose, clientserver, spawnClientserver bool
	var serverhash, add, dspath string
	var catarg, recipient string
	var port int
	flag.BoolVar(&verbose, "v", false, "Verbose")
	flag.StringVar(&recipient, "e", "", "Encrypt or decrypt to PGP recipient")
	flag.StringVar(&dspath, "d", "/tmp/ipboh-data", "Data store path, by default /tmp/ipboh-data")
	flag.StringVar(&serverhash, "h", "", "Server hash to connect to")
	flag.IntVar(&port, "p", 9898, "Port used by localhost client server (9898)")
	flag.BoolVar(&clientserver, "c", false, "Start client server")
	flag.Parse()

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
		r, err := fsrepo.Open("~/.ipfs")
		//if err != nil && strings.Contains(fmt.Sprintf("%s",err),"temporar")
		if err != nil {
			config, err := config.Init(os.Stdout, 2048)
			if err != nil {
				panic(err)
			}

			home := os.Getenv("HOME")
			if err := fsrepo.Init(home+"/.ipfs", config); err != nil {
				panic(err)
			}

			r, err = fsrepo.Open("~/.ipfs")
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
		exePath, err := Readlink("/proc/self/exe")
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

		err := os.Mkdir(dspath, 0700)
		if err != nil {
			fmt.Println("Could not make dspath:", dspath)
		}

		index = loadIndex(dspath)

		go handleIndex(n, ctx, index, &wg)
		go handleAdd(n, ctx, index, &wg, dspath)
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

			newcontent := getNewContent(add)

			if recipient != "" {
				encbytes, err := encryptOpenpgp(newcontent.Content, recipient)
				if err != nil {
					fmt.Println("Failed to encrypt.")
				}
				newcontent.Content = encbytes
			}

			contentbytes, err := json.Marshal(newcontent)

			buf := bytes.NewBuffer(contentbytes)
			resp, err := http.Post(fmt.Sprintf("http://localhost:%d/add?target=%s", port, serverhash), "application/json", buf)

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
				bytes, err = decryptOpenpgp(bytes)
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

			for i := range entrylist.Entries {
				fmt.Println(entrylist.Entries[i].Hash, entrylist.Entries[i].Name)
			}

		}
	}

}
