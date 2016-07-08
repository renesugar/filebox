package filebox_test

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/qor/admin"
	"github.com/qor/filebox"
	"github.com/qor/qor"
	"github.com/qor/roles"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
)

var Filebox *filebox.Filebox
var Admin *admin.Admin
var Server *httptest.Server
var CurrentUser *User
var Root string

// User definition
type User struct {
	Name string
	Role string
}

func (user *User) DisplayName() string {
	return user.Name
}

type AdminAuth struct {
}

func (AdminAuth) LoginURL(c *admin.Context) string {
	return "/auth/login"
}

func (AdminAuth) LogoutURL(c *admin.Context) string {
	return "/auth/logout"
}

func (AdminAuth) GetCurrentUser(c *admin.Context) qor.CurrentUser {
	return CurrentUser
}

// Init
func init() {
	Root, _ = os.Getwd()
	mux := http.NewServeMux()
	Server = httptest.NewServer(mux)
	CurrentUser = &User{Name: "user", Role: "normal_user"}
	roles.Register("admin", func(req *http.Request, currentUser interface{}) bool {
		return currentUser.(*User) != nil && currentUser.(*User).Role == "admin"
	})
	roles.Register("manager", func(req *http.Request, currentUser interface{}) bool {
		return currentUser.(*User) != nil && currentUser.(*User).Role == "manager"
	})

	Filebox = filebox.New(Root + "/test/filebox")
	Filebox.MountTo(mux)
	Filebox.SetAuth(AdminAuth{})
}

func reset() {
	clearFiles()
}

// Test download cases
type filePermission struct {
	DirPermission *roles.Permission
	FileName      string
	AllowRoles    []string
}

type testDownloadCase struct {
	CurrentRole      string
	DownloadURL      string
	ExpectStatusCode int
	ExpectContext    string
}

func TestDownloads(t *testing.T) {
	reset()
	filePermissions := []filePermission{
		filePermission{FileName: "a.csv", AllowRoles: []string{}},
		filePermission{FileName: "b.csv", AllowRoles: []string{"admin"}},
		filePermission{FileName: "c.csv", AllowRoles: []string{"manager", "admin"}},
		filePermission{FileName: "translations/en.csv", AllowRoles: []string{"manager", "admin"}},
		// File doesn't set permission, but Dir set
		filePermission{
			DirPermission: roles.Allow(roles.Read, "admin"),
			FileName:      "translations/users.csv",
			AllowRoles:    []string{},
		},
		// File set permission and Dir set permission too, File's permission will override Dir's permission
		filePermission{
			DirPermission: roles.Allow(roles.Read, "admin"),
			FileName:      "translations/products.csv",
			AllowRoles:    []string{"manager", "admin"},
		},
	}

	testCases := []testDownloadCase{
		testDownloadCase{CurrentRole: "", DownloadURL: "/downloads/missing.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "", DownloadURL: "/downloads/a.csv", ExpectStatusCode: 200, ExpectContext: "Column1,Column2\n"},
		testDownloadCase{CurrentRole: "admin", DownloadURL: "/downloads/a.csv", ExpectStatusCode: 200, ExpectContext: "Column1,Column2\n"},
		testDownloadCase{CurrentRole: "", DownloadURL: "/downloads/b.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "manager", DownloadURL: "/downloads/b.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "admin", DownloadURL: "/downloads/b.csv", ExpectStatusCode: 200, ExpectContext: "Column3,Column4\n"},
		testDownloadCase{CurrentRole: "", DownloadURL: "/downloads/c.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "manager", DownloadURL: "/downloads/c.csv", ExpectStatusCode: 200, ExpectContext: "Column5,Column6\n"},
		testDownloadCase{CurrentRole: "admin", DownloadURL: "/downloads/c.csv", ExpectStatusCode: 200, ExpectContext: "Column5,Column6\n"},
		testDownloadCase{CurrentRole: "", DownloadURL: "/downloads/translations/en.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "manager", DownloadURL: "/downloads/translations/en.csv", ExpectStatusCode: 200, ExpectContext: "Key,Value\n"},
		testDownloadCase{CurrentRole: "admin", DownloadURL: "/downloads/translations/en.csv", ExpectStatusCode: 200, ExpectContext: "Key,Value\n"},
		testDownloadCase{CurrentRole: "", DownloadURL: "/downloads/translations/users.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "manager", DownloadURL: "/downloads/translations/users.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "admin", DownloadURL: "/downloads/translations/users.csv", ExpectStatusCode: 200, ExpectContext: "ID,Name\n"},
		testDownloadCase{CurrentRole: "", DownloadURL: "/downloads/translations/products.csv", ExpectStatusCode: 404, ExpectContext: ""},
		testDownloadCase{CurrentRole: "manager", DownloadURL: "/downloads/translations/products.csv", ExpectStatusCode: 200, ExpectContext: "ID,Code\n"},
		testDownloadCase{CurrentRole: "admin", DownloadURL: "/downloads/translations/products.csv", ExpectStatusCode: 200, ExpectContext: "ID,Code\n"},
	}

	for i, f := range filePermissions {
		if len(f.AllowRoles) > 0 {
			permission := roles.Allow(roles.Read, f.AllowRoles...)
			newFile := Filebox.AccessFile(f.FileName)
			if err := newFile.SetPermission(permission); err != nil {
				t.Errorf(color.RedString(fmt.Sprintf("Filebox: set file permission #(%v) failure (%v)", i+1, err)))
			}
		}
		if f.DirPermission != nil {
			newFile := Filebox.AccessFile(f.FileName)
			newFile.Dir.SetPermission(f.DirPermission)
		}
	}

	for i, testCase := range testCases {
		var hasError bool
		if testCase.CurrentRole == "" {
			CurrentUser = nil
		} else {
			CurrentUser = &User{Name: "Nika", Role: testCase.CurrentRole}
		}
		req, err := http.Get(Server.URL + testCase.DownloadURL)
		if err != nil || req.StatusCode != testCase.ExpectStatusCode {
			t.Errorf(color.RedString(fmt.Sprintf("Download #(%v): status code expect %v, but get %v", i+1, testCase.ExpectStatusCode, req.StatusCode)))
			hasError = true
		}
		if testCase.ExpectContext != "" {
			body, _ := ioutil.ReadAll(req.Body)
			if string(body) != testCase.ExpectContext {
				t.Errorf(color.RedString(fmt.Sprintf("Download #(%v): context expect %v, but get %v", i+1, testCase.ExpectContext, string(body))))
				hasError = true
			}
		}
		if !hasError {
			fmt.Printf(color.GreenString("Download #%v: Success\n", i+1))
		}
	}
}

// Test Put file
type testPutFileCase struct {
	FilePath       string
	Context        string
	ExpectSavePath string
	ExpectContext  string
}

func TestPutFile(t *testing.T) {
	reset()
	testCases := []testPutFileCase{
		testPutFileCase{
			FilePath:       "new/new1.csv",
			Context:        "String: Hello world!",
			ExpectSavePath: "/test/filebox/new/new1.csv",
			ExpectContext:  "Hello world!",
		},
		testPutFileCase{
			FilePath:       "new/new2.csv",
			Context:        "File: a.csv",
			ExpectSavePath: "/test/filebox/new/new2.csv",
			ExpectContext:  "Column1,Column2\n",
		},
		testPutFileCase{
			FilePath:       "jobs/translation.csv",
			Context:        "File: a.csv",
			ExpectSavePath: "/test/filebox/jobs/translation.csv",
			ExpectContext:  "Column1,Column2\n",
		},
		testPutFileCase{
			FilePath:       "jobs/translations/1/file.csv",
			Context:        "File: a.csv",
			ExpectSavePath: "/test/filebox/jobs/translations/1/file.csv",
			ExpectContext:  "Column1,Column2\n",
		},
	}
	for i, testCase := range testCases {
		var reader io.Reader
		if strings.HasPrefix(testCase.Context, "String:") {
			reader = strings.NewReader(strings.Replace(testCase.Context, "String: ", "", 1))
		} else {
			fileName := strings.Replace(testCase.Context, "File: ", "", 1)
			reader, _ = os.Open(Root + "/test/filebox/" + fileName)
		}
		newFile := Filebox.AccessFile(testCase.FilePath)
		err := newFile.Write(reader)
		if err != nil {
			t.Errorf(color.RedString(fmt.Sprintf("Put file #%v: create file %v failure, get error %v", i+1, testCase.ExpectSavePath, err)))
		}
		permission := roles.Allow(roles.Read, "admin")
		err = newFile.SetPermission(permission)
		if err != nil {
			t.Errorf(color.RedString(fmt.Sprintf("Put file #%v: set permission to file %v failure, get error %v", i+1, testCase.ExpectSavePath, err)))
		}
		var hasError bool
		if _, err := os.Stat(Root + testCase.ExpectSavePath); os.IsNotExist(err) {
			hasError = true
			t.Errorf(color.RedString(fmt.Sprintf("Put file #%v: should create %v", i+1, testCase.ExpectSavePath)))
		} else {
			context, _ := ioutil.ReadFile(Root + testCase.ExpectSavePath)
			if string(context) != testCase.ExpectContext {
				t.Errorf(color.RedString(fmt.Sprintf("Put file #%v: context should be as %v, but get %v", i+1, testCase.ExpectContext, string(context))))
			}
		}
		if _, err := os.Stat(Root + testCase.ExpectSavePath + ".meta"); os.IsNotExist(err) {
			hasError = true
			t.Errorf(color.RedString(fmt.Sprintf("Put file #%v: should create %v.meta", i+1, testCase.ExpectSavePath)))
		}
		if !hasError {
			fmt.Printf(color.GreenString("Put file #%v: Success\n", i+1))
		}
	}
	clearFiles()
}

// Test Set permission to a folder and write file
type testPutPermissionCase struct {
	Dir                 string
	DirPermission       *roles.Permission
	CurrentRole         string
	WriteFileName       string
	WriteFileContent    string
	WriteFilePermission *roles.Permission
	ExpectHasError      bool
}

func TestDirPutFile(t *testing.T) {
	reset()
	testCases := []testPutPermissionCase{
		testPutPermissionCase{
			Dir:                 "/public",
			DirPermission:       nil,
			CurrentRole:         "",
			WriteFileName:       "a.csv",
			WriteFileContent:    "Hello",
			WriteFilePermission: nil,
			ExpectHasError:      false,
		},
		testPutPermissionCase{
			Dir:                 "/public",
			DirPermission:       nil,
			CurrentRole:         "admin",
			WriteFileName:       "a.csv",
			WriteFileContent:    "Hello tweak",
			WriteFilePermission: roles.Allow(roles.Update, "admin"),
			ExpectHasError:      false,
		},
		testPutPermissionCase{
			Dir:                 "/public",
			DirPermission:       nil,
			CurrentRole:         "",
			WriteFileName:       "a.csv",
			WriteFileContent:    "Hello tweak failure",
			WriteFilePermission: nil,
			ExpectHasError:      true,
		},
		testPutPermissionCase{
			Dir:                 "/private",
			DirPermission:       roles.Allow(roles.Update, "admin"),
			CurrentRole:         "admin",
			WriteFileName:       "a.csv",
			WriteFileContent:    "Hello",
			WriteFilePermission: nil,
			ExpectHasError:      false,
		},
		testPutPermissionCase{
			Dir:                 "/private",
			DirPermission:       roles.Allow(roles.Update, "admin"),
			CurrentRole:         "",
			WriteFileName:       "a.csv",
			WriteFileContent:    "Hello tweak faliure",
			WriteFilePermission: nil,
			ExpectHasError:      true,
		},
		testPutPermissionCase{
			Dir:                 "/private",
			DirPermission:       roles.Allow(roles.Update, "admin"),
			CurrentRole:         "admin",
			WriteFileName:       "a.csv",
			WriteFileContent:    "Hello tweak",
			WriteFilePermission: nil,
			ExpectHasError:      false,
		},
	}

	for i, testCase := range testCases {
		dir := Filebox.AccessDir(testCase.Dir, testCase.CurrentRole)
		if testCase.DirPermission != nil {
			dir.SetPermission(testCase.DirPermission)
		}
		file, err := dir.WriteFile(testCase.WriteFileName, strings.NewReader(testCase.WriteFileContent))
		var hasError bool
		if testCase.ExpectHasError && err == nil {
			hasError = true
			t.Errorf(color.RedString(fmt.Sprintf("Put Permission #%v: should can't update file, but be updated", i+1)))
		}
		if !testCase.ExpectHasError && err != nil {
			hasError = true
			t.Errorf(color.RedString(fmt.Sprintf("Put Permission #%v: should can update file, but got error %v", i+1, err)))
		}
		if testCase.WriteFilePermission != nil {
			file.SetPermission(testCase.WriteFilePermission)
		}
		if !testCase.ExpectHasError {
			context, _ := ioutil.ReadFile(path.Join(Root, "test/filebox", testCase.Dir, testCase.WriteFileName))
			if string(context) != testCase.WriteFileContent {
				hasError = true
				t.Errorf(color.RedString(fmt.Sprintf("Put Permission #%v: should write context %v, but got %v", i+1, testCase.WriteFileContent, string(context))))
			}
		}
		if !hasError {
			fmt.Printf(color.GreenString("Put Permission #%v: success\n", i+1))
		}
	}
	clearFiles()
}

// Helper
func clearFiles() {
	for _, f := range []string{"a.csv", "b.csv", "c.csv", "translations/en.csv", "translations/products.csv", "translations/"} {
		os.Remove(Root + fmt.Sprintf("/test/filebox/%v.meta", f))
	}
	for _, f := range []string{"jobs", "private", "public", "new"} {
		os.RemoveAll(path.Join(Root, "/test/filebox", f))
	}
}
