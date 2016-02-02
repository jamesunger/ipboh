package main;

import (
	"testing"
	"golang.org/x/net/context"
	"os"
	"strings"
	"io/ioutil"
	"fmt"
	"net/http/httptest"
	"net/http"
	"time"
	//"sync"
	"os/exec"
	core "github.com/ipfs/go-ipfs/core"
	"bytes"

)

const pgpmessage = ` -----BEGIN PGP MESSAGE-----

wcBMA3h+u4j1I5AhAQgAY/E8KiMyHmqZoEJS13KH77/v28Wer+AAG0XzdyH/CfFK
Mr6hyNRqPLTdgLG9c3wMzovQj0ZaH35+zv17hjzTt6OQd6BMPpXsrgjMgg7jEqbQ
1Ap89HvF9Ii86w4o/fPnPbtDuCJQkhUIi0IKWXotlyIU2fUwloJYs7ZNuDlPIUdy
uts0/pbWbVznxo13IeP5b6ZmRUIKxbYHGRA9J9eHkYliKp67Atak5UQwjXaK1uiJ
n9ZGM7Zvmk23sL/hlxWBhOWg914Q+DLcIJGKMM6hbvtNNeyX9VMmOb9oWwkPwGw1
O5YzrydU01Ws2pHAJKcGpooRqrJ2EkxQ3OkmzHz8MdLgAeSNju7oPM9l528yPtA6
pVAm4ZOA4H7gXuGL4OBt4vp8TQLgPOPGJ9g96CjE/eCA4n+AqZfgtOC04DLkCeYk
YJzlMYirBCyaKaMNnOLYXbeq4fGiAA==
=tcDS
-----END PGP MESSAGE-----`

var globalnode *core.IpfsNode
var globalctx context.Context
var testtarget string

func TestMain(m *testing.M) {
	testtarget = os.Getenv("TESTTARGET")
	os.Exit(m.Run())
}

func Test_readIpbohConfig(t *testing.T) {

	fh, err := os.Create("testconf")
	defer fh.Close()
	if err != nil {
		t.Fail()
	}
	fh.Write([]byte("{\"Serverhash\":\"QmBROKENTESTSdkr8QLTjsdflksdjfY4JymQU\",\"Port\":9898}"))

	ipbohconf := readIpbohConfig("testconf")
	err = os.Remove("testconf")

	if ipbohconf.Serverhash != "" {
		return
	}

	t.Fail()

}

func Test_startupIPFS(t *testing.T) {
	ctx, _ := context.WithCancel(context.Background())

	//defer cancel()
	//home := getHomeDir()
	home := "test"
	dspath := fmt.Sprintf("%s/.ipfs", home)
	_, err := startupIPFS(dspath, &ctx)
	if err != nil {
		t.Fail()
	}

	time.Sleep(1*time.Second)

}



func Test_phase1Setup_clserver(t *testing.T) {
	home := "test"
	dspath := fmt.Sprintf("%s/.ipfs", home)
	ctx, _ := context.WithCancel(context.Background())
	globalctx = ctx
	//n,serverhash,port,baseurl,spawnclnt := phase1Setup(globalctx,true,false,false,"test",home,globalnode.Identity.Pretty(), 42334)
	n,_,_,_,_ := phase1Setup(globalctx,false,false,true,dspath,home,testtarget, 42334)
	if n == nil {
		t.Fail()
	}
	globalnode = n
	//time.Sleep(10*time.Second)
}




func Test_basicInit(t *testing.T) {
	dspath, home, gpghomeDefault,_ := basicInit()
	if dspath == "" {
		t.Fail()
	}

	if home == "" {
		t.Fail()
	}

	if gpghomeDefault == "" {
		t.Fail()
	}

}


func Test_parseCommandFromArgs(t *testing.T) {
	server,add,catarg := parseCommandFromArgs()
	if server != false {
		t.Fail()
	}

	if add != "" {
		t.Fail()
	}

	if catarg != "" {
		t.Fail()
	}
}


func Test_encryptOpenpgp(t *testing.T) {
	testbytes := []byte("sometestbytes")
	rdr := bytes.NewReader(testbytes)

	encbytes,err := encryptOpenpgp(rdr,"ipbohtestkey","test")
	if err != nil {
		fmt.Println(err)
		t.Fail()
	}

	if !strings.Contains(string(encbytes), "BEGIN PGP MESSAGE") {
		t.Fail()
	}
}

func Test_decryptOpenpgp(t *testing.T) {

	rdr := bytes.NewReader([]byte(pgpmessage))


	secrdr,err := decryptOpenpgp(rdr,"test",[]byte("ipbohtestkey"))
	if err != nil {
		t.Fail()
	} else {
		secbytes,_ := ioutil.ReadAll(secrdr)
		if len(secbytes) == 0 {
			t.Fail()
		}
	}

}

func Test_saveIndex(t *testing.T) {
	exec.Command("rm", "test/ipboh-index.txt").Output()
	indx := makeIndex()
	indx.Entries = append(indx.Entries, &Entry{ Hash: "testentry", Name: "foobar" })
	saveIndex(indx,"test")
}

func Test_loadIndex(t *testing.T) {
	indx := loadIndex("test")
	if indx == nil {
		t.Fail()
	}
}

func Test_addContent_toolong(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	defer func() {
		if r := recover(); r != nil {
			return
		}
	}()

	addContent("sometest..................................................................................................................................................................................","test","",server.URL,"badhash")
	t.Fail()
}


func Test_addContent_encrypted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	addContent("sometest","test","ipbohtestkey",server.URL,"badhash")
}




func Test_catContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//w.WriteHeader(200)
		fmt.Fprintln(w,"hi there..........................................................................................................")
	}))
	defer server.Close()

	rdr := catContent("sometest",server.URL,"badhash")
	rbytes,err := ioutil.ReadAll(rdr)
	if err != nil {
		t.Fail()
	}

	if len(rbytes) == 0 {
		t.Fail()
	}

}


func Test_listEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintln(w,"{\"Entries\":[{\"Name\":\"foobar\",\"Hash\":\"testentry\",\"Timestamp\":\"0001-01-01T00:00:00Z\",\"Size\":0}]}")
	}))
	defer server.Close()

	listEntries(server.URL,"badhash",false)
}

func Test_listEntries_verbose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintln(w,"{\"Entries\":[{\"Name\":\"foobar\",\"Hash\":\"testentry\",\"Timestamp\":\"0001-01-01T00:00:00Z\",\"Size\":0}]}")
	}))
	defer server.Close()

	listEntries(server.URL,"badhash",true)
}

func Test_hasHideList_true(t *testing.T) {
	indx := makeIndex()
	indx.Entries = append(indx.Entries, &Entry{ Hash: "testentry", Name: "hidelist" })
	if !hasHideList(indx) {
		t.Fail()
	}
}

func Test_hasHideList_false(t *testing.T) {
	indx := makeIndex()
	indx.Entries = append(indx.Entries, &Entry{ Hash: "testentry", Name: "foobar" })
	if hasHideList(indx) {
		t.Fail()
	}
}

func Test_hidelist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintln(w,"")
	}))
	defer server.Close()

	indx := makeIndex()
	indx.Entries = append(indx.Entries, &Entry{ Hash: "testentry", Name: "hidelist" })
	hidelist := getHideList("badhash",server.URL,indx)
	if len(hidelist) == 0 {
		t.Fail()
	}
}

func Test_waitForClientserver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	err := waitForClientserver(5,server.URL)
	if err != nil {
		t.Fail()
	}

}

func Test_clientHandlerAdd(t *testing.T) {
	newrec := httptest.NewRecorder()


	rdr := bytes.NewReader([]byte("testcontentadd                                                                                                                      boo here is some content!"))
	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	clientHandlerAdd(newrec, rdr, globalnode, testtarget)

}

func Test_clientHandlerAdd_encrypted(t *testing.T) {
	newrec := httptest.NewRecorder()

	rdr := bytes.NewReader([]byte("testcontentadd                                                                                                                      "+pgpmessage))
	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	clientHandlerAdd(newrec, rdr, globalnode, testtarget)

}









func Test_startClientServer(t *testing.T) {
	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	go startClientServer(globalctx,globalnode,"http://127.0.0.1:42334",testtarget)
	time.Sleep(1*time.Second)

	resp,err := http.Get("http://127.0.0.1:42334/areuthere")
	if err != nil {
		t.Fail()
	}
	//fmt.Println(resp.Status)

	resp,err = http.Get("http://127.0.0.1:42334/")
	if err != nil {
		t.Fail()
	}

	if resp.StatusCode != 200 {
		t.Fail()
	}


}

func Test_clientHandlerIndex(t *testing.T) {
	newrec := httptest.NewRecorder()


	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	clientHandlerIndex(newrec, globalctx, globalnode, "http://127.0.0.1:42334",testtarget,false)
	clientHandlerIndex(newrec, globalctx, globalnode, "http://127.0.0.1:42334",testtarget,true)

}


func Test_clientHandlerLs(t *testing.T) {
	newrec := httptest.NewRecorder()


	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	clientHandlerLs(newrec, globalnode, testtarget)

}






func Test_clientHandlerCat(t *testing.T) {
	newrec := httptest.NewRecorder()

	testhash := "QmUkQEWvMVTn1xSEFqKvcSC9a5nAJcuJdBLohx33KvZcf7"
	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	clientHandlerCat(globalctx, newrec, globalnode, testhash, testtarget)
	//fmt.Println(string(newrec.Body.Bytes()))
	if len(newrec.Body.Bytes()) == 0{
		t.Fail()
	}
}

func Test_clientHandlerCat_encrypted(t *testing.T) {
	newrec := httptest.NewRecorder()

	testhash := "QmUkQEWvMVTn1xSEFqKvcSC9a5nAJcuJdBLohx33KvZcf7"
	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	clientHandlerCat(globalctx, newrec, globalnode, testhash, testtarget)
	//fmt.Println(string(newrec.Body.Bytes()))

	catCatContent(newrec.Body,os.Stdout,"test","ipbohtestkey")
	/*if len(newrec.Body.Bytes()) == 0{
		t.Fail()
	}*/



}



func Test_clientHandlerCat_byname(t *testing.T) {
	newrec := httptest.NewRecorder()

	testhash := "ipboh.go"
	if testtarget == "" {
		fmt.Println("target ipboh node is null, skipping.")
		return
	}
	clientHandlerCat(globalctx, newrec, globalnode, testhash, testtarget)
	if len(newrec.Body.Bytes()) == 0{
		t.Fail()
	}
}



func Test_getUpdateConfig(t *testing.T) {
	filepath := fmt.Sprintf("%s.ipbohrc","test/")
	serverhash, port := getUpdateConfig(filepath, globalnode.Identity.Pretty(), 9898)
	if serverhash == "" {
		t.Fail()
	}

	if port == 0 {
		t.Fail()
	}
}

func Test_saveIpbohConfig(t *testing.T) {
	filepath := fmt.Sprintf("%s.ipbohrc","test/")
	ipcfg := readIpbohConfig(filepath)


	saveIpbohConfig(ipcfg,filepath)

}

func Test_serverContentReader(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("WAS PANICKING",r)
			return
		}
	}()
	rdr := bytes.NewReader([]byte("some random stuffxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"))
	scr := &serverContentReader{r: rdr,namebytes: []byte("stuff")}



	obytes,err := ioutil.ReadAll(scr)
	if err != nil {
		fmt.Println(err)
		t.Fail()
	}

	fmt.Println(string(obytes))
	if len(obytes) == 0 {
		t.Fail()
	}


}


func Test_getGpghome(t *testing.T) {
	home := getHomeDir()
	gpghomeDefault := getGpghomeDir(home)

	if home == "" || gpghomeDefault == "" {
		t.Fail()
	}

}

func Test_processClientCommands(t *testing.T) {
	processClientCommands(false,testtarget,"","","test","","",false,"http://127.0.0.1:42334")
	processClientCommands(false,testtarget,"addstring","","test","","",false,"http://127.0.0.1:42334")
	processClientCommands(false,testtarget,"","catarg","test","","",false,"http://127.0.0.1:42334")
}


func Test_server(t *testing.T) {
	_, _, _,wg := basicInit()
	dspath := "test"

	go startServer(globalctx,globalnode,dspath,&wg)

	time.Sleep(1*time.Second)
}

