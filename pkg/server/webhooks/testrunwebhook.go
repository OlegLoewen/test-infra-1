// Copyright 2019 Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package webhooks

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	tmv1beta1 "github.com/gardener/test-infra/pkg/apis/testmachinery/v1beta1"
	"github.com/gardener/test-infra/pkg/testmachinery/testrun"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// NewTestrunWebhook creates a new validation webhook for tesetruns with a controller-runtime decoder
func NewTestrunWebhook() (*TestrunWebhook, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := admissionregistrationv1beta1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	decoder, err := admission.NewDecoder(scheme)
	if err != nil {
		return nil, err
	}

	return &TestrunWebhook{*decoder, serializer.NewCodecFactory(scheme)}, nil
}

// Validate handles a admission webhook request for testruns.
func (wh *TestrunWebhook) Validate(w http.ResponseWriter, r *http.Request) {

	const wantedContentType = "application/json"

	receivedReview := v1beta1.AdmissionReview{}
	deserializer := wh.codecs.UniversalDecoder()
	var body []byte

	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// Verify that the correct content-type header has been sent.
	if contentType := r.Header.Get("Content-Type"); contentType != wantedContentType {
		log.Errorf("contentType=%s, expect %s", contentType, wantedContentType)
		res := admission.Errored(http.StatusConflict, fmt.Errorf("expect %s", wantedContentType))
		respond(w, &res.AdmissionResponse)
		return
	}

	// Deserialize HTTP request body into admissionv1beta1.AdmissionReview object.
	if _, _, err := deserializer.Decode(body, nil, &receivedReview); err != nil {
		log.Error(err.Error())
		res := admission.Errored(http.StatusConflict, err)
		respond(w, &res.AdmissionResponse)
		return
	}

	if receivedReview.Request == nil {
		err := fmt.Errorf("invalid request body (missing admission request)")
		log.Error(err.Error())
		res := admission.Errored(http.StatusConflict, err)
		respond(w, &res.AdmissionResponse)
		return
	}

	if receivedReview.Request.Operation != v1beta1.Create {
		res := admission.ValidationResponse(true, "No validation needed")
		respond(w, &res.AdmissionResponse)
		return
	}

	tr := &tmv1beta1.Testrun{}
	if err := wh.d.Decode(admission.Request{AdmissionRequest: *receivedReview.Request}, tr); err != nil {
		log.Error(err.Error())
		res := admission.Errored(http.StatusConflict, err)
		respond(w, &res.AdmissionResponse)
		return
	}

	if err := testrun.Validate(tr); err != nil {
		err := fmt.Errorf("Invalid Testrun %s: %s", tr.Name, err.Error())
		log.Error(err.Error())
		res := admission.ValidationResponse(false, err.Error())
		respond(w, &res.AdmissionResponse)
		return
	}

	log.Infof("Successfully validated Testrun %s", tr.Name)
	res := admission.ValidationResponse(true, "No reason")
	respond(w, &res.AdmissionResponse)
}

func respond(w http.ResponseWriter, response *v1beta1.AdmissionResponse) {
	responseObj := v1beta1.AdmissionReview{}
	if response != nil {
		responseObj.Response = response
	}

	jsonResponse, err := json.Marshal(responseObj)
	if err != nil {
		log.Error(err)
	}
	if _, err := w.Write(jsonResponse); err != nil {
		log.Error(err)
	}
}
