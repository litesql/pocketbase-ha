package files

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

type resolvedFile struct {
	collection *core.Collection
	servedPath string
	servedName string
}

func authorizeFileRequest(e *core.RequestEvent) (*resolvedFile, error) {
	collectionParam, recordID, filename, thumb, ok := ParseFilesURL(e.Request.URL)
	if !ok {
		return nil, e.NotFoundError("", nil)
	}

	collection, err := e.App.FindCachedCollectionByNameOrId(collectionParam)
	if err != nil {
		return nil, e.NotFoundError("", nil)
	}

	record, err := e.App.FindRecordById(collection, recordID)
	if err != nil {
		return nil, e.NotFoundError("", err)
	}

	fileField := record.FindFileFieldByFile(filename)
	if fileField == nil {
		return nil, e.NotFoundError("", nil)
	}

	if fileField.Protected {
		originalRequestInfo, err := e.RequestInfo()
		if err != nil {
			return nil, e.InternalServerError("Failed to load request info", err)
		}

		token := e.Request.URL.Query().Get("token")
		authRecord, _ := e.App.FindAuthRecordByToken(token, core.TokenTypeFile)

		if authRecord != nil && authRecord.IsSuperuser() {
			allowedIPs := e.App.Settings().SuperuserIPs
			if len(allowedIPs) > 0 && !ipInList(allowedIPs, e.RealIP()) {
				authRecord = nil
			}
		}

		requestInfo := *originalRequestInfo
		requestInfo.Context = core.RequestInfoContextProtectedFile
		requestInfo.Auth = authRecord

		if ok, _ := e.App.CanAccessRecord(record, &requestInfo, record.Collection().ViewRule); !ok {
			return nil, e.NotFoundError("", errors.New("insufficient permissions to access the file resource"))
		}
	}

	baseFilesPath := record.BaseFilesPath()
	if collection.IsView() {
		fileRecord, err := e.App.FindRecordByViewFile(collection.Id, fileField.Name, filename)
		if err != nil {
			return nil, e.NotFoundError("", err)
		}
		baseFilesPath = fileRecord.BaseFilesPath()
	}

	servedPath := baseFilesPath + "/" + filename
	servedName := filename
	if shouldUseThumbPath(thumb, fileField.Thumbs, filename) {
		servedName = thumb + "_" + filename
		servedPath = baseFilesPath + "/thumbs_" + filename + "/" + servedName
	}

	return &resolvedFile{collection: collection, servedPath: servedPath, servedName: servedName}, nil
}

func ipInList(ipsOrSubnets []string, ip string) bool {
	if len(ipsOrSubnets) == 0 || ip == "" {
		return false
	}
	searchAddr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, item := range ipsOrSubnets {
		if prefix, err := netip.ParsePrefix(item); err == nil {
			if prefix.Contains(searchAddr) {
				return true
			}
			continue
		}
		if addr, err := netip.ParseAddr(item); err == nil && addr == searchAddr {
			return true
		}
	}
	return false
}

func isFilesDownload(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return strings.HasPrefix(r.URL.Path, filesPathPrefix)
}
