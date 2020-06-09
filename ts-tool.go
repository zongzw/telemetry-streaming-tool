package main

import (
	"fmt"
	"net/http"
	"crypto/tls"
	"log"
	"io/ioutil"
	"bytes"
	"os"
	"time"
	"strings"
	// "path/filepath"
	"github.com/tidwall/gjson"
	"encoding/json"
	"encoding/base64"
	"flag"
)

type VerifiedTSInfo struct {
	NodeVersion string
	Version string
	Release string
	SchemaCurrent string
	SchemaMinimum string
}

type Schedule struct {
	Targets []string
	Credential string
	Template string
	Version string
}

type Package struct {
	Version string
	Remotepath string
	Filepath string
	Sha256sum string
	Name string
	Release string
	Arch string
	PackageName string
}

type Target struct {
	Ipaddr string
	Credential string
}

func panic(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func Upload(client *http.Client, tgt *Target, pkg *Package) {
	ps := strings.Split(pkg.Filepath, "/")
	pkgName := ps[len(ps)-1]

	// TODO: use absolute path
	pkgPath := pkg.Filepath
	finfo, err := os.Stat(pkgPath)
	if os.IsNotExist(err) {
		panic(err)
	}

	// TODO: check file's sha256sum

	uploadUrl := fmt.Sprintf(
		`https://%s/mgmt/shared/file-transfer/uploads/%s`, 
		tgt.Ipaddr,
		pkgName,
	)

	uploadBody, err := ioutil.ReadFile(pkgPath)
	reqUpload, err := http.NewRequest(
		"POST", 
		uploadUrl, 
		bytes.NewReader(uploadBody),
	)

	// TODO: use sched.Credential
	reqUpload.Header.Add("Authorization", tgt.Credential)
	reqUpload.Header.Add("Content-Type", "application/octet-stream")
	reqUpload.Header.Add(
		"Content-Range", 
		fmt.Sprintf(`0-%d/%d`, finfo.Size()-1, finfo.Size()),
	)
	reqUpload.Header.Add("Content-Length", string(finfo.Size()))
	reqUpload.Header.Add("Connection", "keep-alive")

	respUpload, err := client.Do(reqUpload)
	panic(err)
	defer respUpload.Body.Close()

	if int(respUpload.StatusCode / 200) != 1 {
		rsp, _ := ioutil.ReadAll(respUpload.Body)
		panic(
			fmt.Errorf(
				"target %s uploading TS package failed: %d, %s", 
				tgt.Ipaddr, respUpload.StatusCode, rsp,
			),
		)
	} else {
		log.Printf("target %s uploaded TS package %s", tgt.Ipaddr, pkgName)
	}
}

func Install(client *http.Client, tgt *Target, pkg *Package) {
	ps := strings.Split(pkg.Filepath, "/")
	pkgName := ps[len(ps)-1]

	installUrl := fmt.Sprintf(
		"https://%s/mgmt/shared/iapp/package-management-tasks",
		tgt.Ipaddr,
	)
	installBody := fmt.Sprintf(`{
			"operation": "INSTALL",
			"packageFilePath": "/var/config/rest/downloads/%s"
		}`, 
		pkgName,
	)

	reqInstall, err := http.NewRequest(
		"POST",
		installUrl,
		strings.NewReader(installBody),
	)
	panic(err)

	reqInstall.Header.Add("Content-Type", "application/json;charset=UTF-8")
	reqInstall.Header.Add("Authorization", tgt.Credential)
	
	respInstall, err := client.Do(reqInstall)
	panic(err)
	defer respInstall.Body.Close()

	if int(respInstall.StatusCode / 200) != 1 {
		rsp, _ := ioutil.ReadAll(respInstall.Body)
		panic(
			fmt.Errorf(
				"target %s installing TS package failed: %d, %s", 
				tgt.Ipaddr, respInstall.StatusCode, rsp,
			),
		)
	} else {
		log.Printf("target %s installed TS package %s", tgt.Ipaddr, pkgName)
	}
}

func Verify(client *http.Client, tgt *Target) *VerifiedTSInfo {

	verifyUrl := fmt.Sprintf(
		`https://%s/mgmt/shared/telemetry/info`, tgt.Ipaddr,
	)

	reqVerify, err := http.NewRequest("GET", verifyUrl, nil)
	panic(err)

	reqVerify.Header.Add("Authorization", tgt.Credential)
	
	respVerify, err := client.Do(reqVerify)
	panic(err)
	defer respVerify.Body.Close()

	vts := VerifiedTSInfo{}
	if respVerify.StatusCode != 200 {
		body, _ := ioutil.ReadAll(respVerify.Body)
		log.Printf(
			"target %s returns %d, body: %s", 
			tgt.Ipaddr, respVerify.StatusCode, string(body),
		)
	} else {
		verifed, _ := ioutil.ReadAll(respVerify.Body)
		err := json.Unmarshal(verifed, &vts)
		panic(err)
	}

	return &vts;
}

func Deploy(client *http.Client, tgt *Target, declaration []byte) {
	deployUrl := fmt.Sprintf(
		"https://%s/mgmt/shared/telemetry/declare", tgt.Ipaddr,
	)

	reqDeploy, err := http.NewRequest(
		"POST", deployUrl, bytes.NewReader(declaration),
	)

	reqDeploy.Header.Add("Authorization", tgt.Credential)

	respDeploy, err := client.Do(reqDeploy)
	panic(err)

	defer respDeploy.Body.Close()

	if int(respDeploy.StatusCode / 200) != 1 {
		reason, _ := ioutil.ReadAll(respDeploy.Body)
		panic(
			fmt.Errorf("target %s deploy template failed: %d, %s.", 
				tgt.Ipaddr, respDeploy.StatusCode, reason,
			),
		)
	} else {
		log.Printf("target %s deployed template successfully", tgt.Ipaddr)
	}
}

func GetInstalledPkgs(client *http.Client, tgt *Target) []Package {
	queryUrl := fmt.Sprintf(
		"https://%s/mgmt/shared/iapp/package-management-tasks",
		tgt.Ipaddr,
	)

	queryBody := []byte(`{ "operation": "QUERY" }`)
	reqGetPkgs, err := http.NewRequest(
		"POST", queryUrl, bytes.NewReader(queryBody),
	)
	panic(err)

	reqGetPkgs.Header.Add("Authorization", tgt.Credential)

	respGetPkgs, err := client.Do(reqGetPkgs)
	panic(err)

	defer respGetPkgs.Body.Close()
	bd, _ := ioutil.ReadAll(respGetPkgs.Body)

	if int(respGetPkgs.StatusCode / 200) != 1 {	
		panic(
			fmt.Errorf(
				"target %s failed to create query task: %s.",
				tgt.Ipaddr, bd,
			),
		)
	}

	queryid := gjson.GetBytes(bd, "id")
	queryUrl = fmt.Sprintf(
		"https://%s/mgmt/shared/iapp/package-management-tasks/%s",
		tgt.Ipaddr, queryid,
	)
	reqGetPkgs, err = http.NewRequest("GET", queryUrl, nil)
	panic(err)

	reqGetPkgs.Header.Add("Authorization", tgt.Credential)

	respGetPkgs, err = client.Do(reqGetPkgs)
	panic(err)
	defer respGetPkgs.Body.Close()

	bd, _ = ioutil.ReadAll(respGetPkgs.Body)
	if respGetPkgs.StatusCode != 200 {
		panic(
			fmt.Errorf(
				"target %s failed to get query task result: %s.",
				tgt.Ipaddr, bd,
			),
		)
	}

	pkgsBytes := gjson.GetBytes(bd, "queryResponse")

	var pkgList []Package
	e := json.Unmarshal([]byte(pkgsBytes.Raw), &pkgList)
	panic(e)

	return pkgList
}

func ResultOfPkgMgmtTask(
	client *http.Client, tgt *Target, taskId string,
) (int, []byte, error) {
	taskUrl := fmt.Sprintf(
		"https://%s/mgmt/shared/iapp/package-management-tasks/%s",
		tgt.Ipaddr, taskId,
	)

	reqTask, err := http.NewRequest("GET", taskUrl, nil)
	panic(err)

	reqTask.Header.Add("Authorization", tgt.Credential)

	respTask, err := client.Do(reqTask)
	panic(err)
	defer respTask.Body.Close()

	bd, _ := ioutil.ReadAll(respTask.Body)
	return respTask.StatusCode, bd, nil
}

func Uninstall(client *http.Client, tgt *Target) {
	pkgList := GetInstalledPkgs(client, tgt)

	for _, pkg := range pkgList {
		if pkg.Name == "f5-telemetry" {
			uninstallUrl := fmt.Sprintf(
				"https://%s/mgmt/shared/iapp/package-management-tasks",
				tgt.Ipaddr,
			)
			uninstallBody := fmt.Sprintf(`
				{
					"operation": "UNINSTALL",
					"packageName": "%s"
				}
				`, pkg.PackageName)
			uninstallReq, err := http.NewRequest(
				"POST", uninstallUrl, strings.NewReader(uninstallBody),
			)
			panic(err)

			uninstallReq.Header.Add("Authorization", tgt.Credential)

			respUninstall, err := client.Do(uninstallReq)
			panic(err)
			defer respUninstall.Body.Close()

			bd, _ := ioutil.ReadAll(respUninstall.Body)
			if int(respUninstall.StatusCode / 200) != 1 {
				panic(
					fmt.Errorf(
						"target %s failed to create uninstall task: %s.",
						tgt.Ipaddr, bd,
					),
				)
			}

			log.Printf(
				"target %s uninstalled package %s", 
				tgt.Ipaddr, 
				pkg.PackageName,
			)
			// TODO: check uninstall status
			// taskId := gjson.GetBytes(bd, "status")
			// ResultOfPkgMgmtTask(client, tgt, string(taskId.Raw))
			break
		}
	}
}

// TODO: Undeploy

func Setup(
	channel chan string, 
	client *http.Client, 
	tgt *Target, 
	pkg *Package, 
	t []byte,
	rlt map[string]string,
) {

	channel <- tgt.Ipaddr

	vts := Verify(client, tgt)
	if vts.Version != pkg.Version {
		log.Printf(
			"target %s TS version is not %s, installing ...",
			tgt.Ipaddr, pkg.Version,
		)

		Upload(client, tgt, pkg)
		Install(client, tgt, pkg)
	} else {
		log.Printf(
			"target %s TS version matched %s, skip.",
			tgt.Ipaddr, pkg.Version,
		)
	}

	if len(t) != 0 {
		i := 0
		wait := 20
		for i < wait {
			vts := Verify(client, tgt)
			if vts.Version == pkg.Version {
				break
			}
			time.Sleep(1 * time.Second)
			i ++
		}
		if i == wait {
			panic(fmt.Errorf("TS endpoint not available"))
		}
		Deploy(client, tgt, t)
	} else {
		log.Printf(
			"target %s skips deploying TS declaration.",
			tgt.Ipaddr,
		)
	}
	
	<- channel
}

func Teardown(
	channel chan string, 
	client *http.Client, 
	tgt *Target, 
	rlt map[string]string,
) {
	channel <- tgt.Ipaddr

	Uninstall(client, tgt)
	<- channel
}

func NewClient() *http.Client {
	tr :=  &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := http.Client{
		Transport: tr,
		Timeout: time.Duration(2 * time.Minute),
	}

	return &client
}

func PackageOf(confCnt []byte, name string) *Package {
	bpkg := gjson.GetBytes(
		confCnt, 
		fmt.Sprintf(
			"packages.%s", 
			strings.ReplaceAll(name, ".", "\\."),
		),
	)
	if !bpkg.Exists() {
		panic(fmt.Errorf("%s", "the error test for fmt.Errorf"))
	}
	var pkg Package
	e := json.Unmarshal([]byte(bpkg.Raw), &pkg)
	panic(e)
	
	pkg.Version = name

	return &pkg
}

func TemplateOf(confCnt []byte, name string) []byte {
	if name == "" {
		return []byte{}
	}

	tmpl := gjson.GetBytes(
		confCnt, 
		fmt.Sprintf(
			"templates.%s", 
			strings.ReplaceAll(name, ".", "\\."),
		),
	)
	if !tmpl.Exists() {
		panic(
			fmt.Errorf("template %s not defined.", name),
		)
	}
	
	return []byte(tmpl.Raw)
}

func TargetOf(ipaddr string, cred string) *Target {
	b64 := base64.StdEncoding.EncodeToString([]byte(cred))
	tgt := Target{
		Ipaddr: ipaddr,
		Credential: fmt.Sprintf("Basic %s", b64),
	}
	return &tgt
}

// type EntryForSetup struct {
// 	Tgt string
// 	Tmpl []byte
// 	Pkg Package
// 	Cred string
// }

// func GetSetupEntry(confCnt []byte, ipaddr string, sched Schedule) {

// }

func main() {
	fmt.Println("Setting up Telemetry Streaming ...")
	// fmt.Println(os.Args)

	var destroy bool
	flag.BoolVar(&destroy, "t", false, "Uninstall for all targets.")

	flag.Parse()

	// dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	// panic(err)
	// path := filepath.Join(dir, "ts-settings.json")
	// TODO: use absolute path to ts-settings.json
	path := "./ts-settings.json"

	confCnt, err := ioutil.ReadFile(path)
	panic(err)
	if !gjson.Valid(string(confCnt)) {
		panic(fmt.Errorf("Invalid json format: %s", path))
	}

	var schedules []Schedule
	d := gjson.GetBytes(confCnt, "schedules")
	e := json.Unmarshal([]byte(d.Raw), &schedules)
	panic(e)

	client := NewClient()

	// TODO: test big ops -> 1000
	ops := make(chan string, 2)
	defer close(ops)
	count := 0
	result := map[string]string{}

	A := func () {
		for _, s := range schedules {
			// TODO: removing duplicate ipaddrs in s.Targets
			// TODO: expending ipaddr range like: 10.145.74.75-79 
			for _, ipaddr := range s.Targets {
				i := TargetOf(ipaddr, s.Credential)
				p := PackageOf(confCnt, s.Version)
				t := TemplateOf(confCnt, s.Template)
				if !destroy {
					go Setup(ops, client, i, p, t, result)
				} else {
					go Teardown(ops, client, i, result)
				}

				count ++
			}
		}
	}

	// B := func () {
	// 	i := 0
	// 	for el :=range ops {
	// 		fmt.Println(el)
	// 		i ++
	// 		if i == count {
	// 			close(ops)
	// 		}
	// 	}
	// }

	go A()
	// B()

	fmt.Print("Enter for quit: ")
	fmt.Scanln()
}
