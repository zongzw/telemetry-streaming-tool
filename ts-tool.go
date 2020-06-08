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
	Remotepath string
	Filepath string
	Sha256sum string
}

func panic(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func Upload(client *http.Client, ipaddr string, pkg *Package) {
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
		ipaddr,
		pkgName,
	)

	uploadBody, err := ioutil.ReadFile(pkgPath)
	reqUpload, err := http.NewRequest(
		"POST", 
		uploadUrl, 
		bytes.NewReader(uploadBody),
	)

	// TODO: use sched.Credendial
	reqUpload.Header.Add("Authorization", "Basic YWRtaW46YWRtaW4=")
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
				ipaddr, respUpload.StatusCode, rsp,
			),
		)
	} else {
		log.Printf("target %s uploaded TS package %s", ipaddr, pkgName)
	}
}

func Install(client *http.Client, ipaddr string, pkg *Package) {
	ps := strings.Split(pkg.Filepath, "/")
	pkgName := ps[len(ps)-1]

	installUrl := fmt.Sprintf(
		"https://%s/mgmt/shared/iapp/package-management-tasks",
		ipaddr,
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
	reqInstall.Header.Add("Authorization", "Basic YWRtaW46YWRtaW4=")
	
	respInstall, err := client.Do(reqInstall)
	panic(err)
	defer respInstall.Body.Close()

	if int(respInstall.StatusCode / 200) != 1 {
		rsp, _ := ioutil.ReadAll(respInstall.Body)
		panic(
			fmt.Errorf(
				"target %s installing TS package failed: %d, %s", 
				ipaddr, respInstall.StatusCode, rsp,
			),
		)
	} else {
		log.Printf("target %s installed TS package %s", ipaddr, pkgName)
	}
}

func Verify(client *http.Client, ipaddr string) *VerifiedTSInfo {

	verifyUrl := fmt.Sprintf(`https://%s/mgmt/shared/telemetry/info`, ipaddr)

	reqVerify, err := http.NewRequest("GET", verifyUrl, nil)
	panic(err)

	reqVerify.Header.Add("Authorization", "Basic YWRtaW46YWRtaW4=")
	
	respVerify, err := client.Do(reqVerify)
	panic(err)
	defer respVerify.Body.Close()

	vts := VerifiedTSInfo{}
	if respVerify.StatusCode != 200 {
		body, _ := ioutil.ReadAll(respVerify.Body)
		log.Printf(
			"Getting TS version from %s returns %d, body: %s", 
			ipaddr, respVerify.StatusCode, string(body),
		)
	} else {
		verifed, _ := ioutil.ReadAll(respVerify.Body)
		err := json.Unmarshal(verifed, &vts)
		panic(err)
	}

	return &vts;
}

func Deploy(client *http.Client, ipaddr string, declaration []byte) {
	deployUrl := fmt.Sprintf(
		"https://%s/mgmt/shared/telemetry/declare", ipaddr,
	)

	reqDeploy, err := http.NewRequest(
		"POST", deployUrl, bytes.NewReader(declaration),
	)

	reqDeploy.Header.Add("Authorization", "Basic YWRtaW46YWRtaW4=")

	respDeploy, err := client.Do(reqDeploy)
	panic(err)

	defer respDeploy.Body.Close()

	if int(respDeploy.StatusCode / 200) != 1 {
		reason, _ := ioutil.ReadAll(respDeploy.Body)
		panic(
			fmt.Errorf("target %s deploy template failed: %d, %s.", 
			ipaddr, respDeploy.StatusCode, reason,
			),
		)
	} else {
		log.Printf("target %s deployed template successfully", ipaddr)
	}
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

/**
	1. parse json body
	2. for loop to do 3-
	3. check target ts version.
	4. if not installed or version not match:
		install
	5. if installed and match:
		deploy ts setting.
	
 */

func main() {
	fmt.Println("Setting up Telemetry Streaming ...")
	// fmt.Println(os.Args)

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
	for _, s := range schedules {
		for _, ipaddr := range s.Targets {
			vts := Verify(client, ipaddr)
			if vts.Version != s.Version {
				log.Printf(
					"target %s TS version is not %s, installing ...",
					ipaddr, s.Version,
				)

				bpkg := gjson.GetBytes(
					confCnt, 
					fmt.Sprintf(
						"packages.%s", 
						strings.ReplaceAll(s.Version, ".", "\\."),
					),
				)
				if !bpkg.Exists() {
					panic(fmt.Errorf("%s", "the error test for fmt.Errorf"))
				}
				var pkg Package
				e := json.Unmarshal([]byte(bpkg.Raw), &pkg)
				panic(e)

				Upload(client, ipaddr, &pkg)
				Install(client, ipaddr, &pkg)
			} else {
				log.Printf(
					"target %s TS version matched %s, skip.",
					ipaddr, s.Version,
				)
			}
			if s.Template != "" {
				i := 0
				for i < 20 {
					vts := Verify(client, ipaddr)
					if vts.Version == s.Version {
						break
					}
					time.Sleep(1 * time.Second)
					i ++
				}
				if i == 5 {
					panic(fmt.Errorf("TS endpoint not available"))
				}

				tmpl := gjson.GetBytes(
					confCnt, 
					fmt.Sprintf(
						"templates.%s", 
						strings.ReplaceAll(s.Template, ".", "\\."),
					),
				)
				if !tmpl.Exists() {
					panic(
						fmt.Errorf("template %s not defined.", s.Template),
					)
				} else {
					Deploy(client, ipaddr, []byte(tmpl.Raw))
				}
			} else {
				log.Printf(
					"target %s skips deploying TS declaration.",
					ipaddr,
				)
			}
		}
	}
}
