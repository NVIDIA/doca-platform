/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package redfish

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

func truststorePath(id int) string {
	return fmt.Sprintf("/redfish/v1/Managers/%s/Truststore/Certificates/%d", ManagerID, id)
}

func (s *Server) handleGenerateCSR(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CommonName       string   `json:"CommonName"`
		AlternativeNames []string `json:"AlternativeNames"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	if body.CommonName == "" {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.PropertyMissing", "CommonName is required")
		return
	}
	csr, err := s.certs.generateCSR(body.CommonName, body.AlternativeNames)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Base.1.18.1.InternalError", err.Error())
		return
	}
	klog.InfoS("Redfish GenerateCSR", "commonName", body.CommonName, "alternativeNames", body.AlternativeNames)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"CSRString":             csr,
		"CertificateCollection": odata("/redfish/v1/Managers/" + ManagerID + "/NetworkProtocol/HTTPS/Certificates"),
	})
}

// handleReplaceCertificate installs a certificate. CertificateUri decides whether it replaces the
// HTTPS server certificate or a truststore CA entry.
func (s *Server) handleReplaceCertificate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CertificateString string `json:"CertificateString"`
		CertificateURI    struct {
			ODataID string `json:"@odata.id"`
		} `json:"CertificateUri"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	uri := body.CertificateURI.ODataID
	if strings.Contains(uri, "/Truststore/Certificates/") {
		id, err := strconv.Atoi(uri[strings.LastIndex(uri, "/")+1:])
		if err != nil {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.PropertyValueFormatError", "invalid CertificateUri "+uri)
			return
		}
		s.state.ReplaceTruststoreCert(id, body.CertificateString)
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	if err := s.certs.replace(body.CertificateString); err != nil {
		klog.ErrorS(err, "Redfish ReplaceCertificate rejected")
		writeError(w, http.StatusInternalServerError, "Base.1.18.1.InternalError", err.Error())
		return
	}
	klog.InfoS("Redfish ReplaceCertificate installed a new HTTPS server certificate")
	writeJSON(w, http.StatusOK, map[string]interface{}{})
}

func (s *Server) handleServerCert(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":         r.URL.Path,
		"@odata.type":       "#Certificate.v1_7_0.Certificate",
		"Id":                "1",
		"CertificateString": s.certs.currentPEM(),
		"CertificateType":   "PEM",
	})
}

func (s *Server) handleTruststoreCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			CertificateString string `json:"CertificateString"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
			return
		}
		if body.CertificateString == "" {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.PropertyMissing", "CertificateString is required")
			return
		}
		id := s.state.AddTruststoreCert(body.CertificateString)
		klog.InfoS("Redfish truststore certificate installed", "id", id)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"@odata.id":       truststorePath(id),
			"@odata.type":     "#Certificate.v1_7_0.Certificate",
			"Id":              strconv.Itoa(id),
			"CertificateType": "PEM",
		})
		return
	}
	ids := s.state.TruststoreIDs()
	members := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		members = append(members, odata(truststorePath(id)))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           r.URL.Path,
		"@odata.type":         "#CertificateCollection.CertificateCollection",
		"Members":             members,
		"Members@odata.count": len(members),
	})
}

func (s *Server) handleTruststoreCert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	if r.Method == http.MethodDelete {
		if !s.state.DeleteTruststoreCert(id) {
			writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	pem, ok := s.state.TruststoreCert(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":         r.URL.Path,
		"@odata.type":       "#Certificate.v1_7_0.Certificate",
		"Id":                strconv.Itoa(id),
		"CertificateString": pem,
		"CertificateType":   "PEM",
	})
}
