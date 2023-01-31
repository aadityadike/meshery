package handlers

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/layer5io/meshkit/models/meshmodel"
	"github.com/layer5io/meshkit/models/meshmodel/core/types"
	"github.com/layer5io/meshkit/models/meshmodel/core/v1alpha1"
)

// swagger:route GET /api/meshmodel/model/{model}/relationship/{name} GetMeshmodelRelationshipByName idGetMeshmodelRelationshipByName
// Handle GET request for getting meshmodel relationships of a specific model by name.
// Example: /api/meshmodel/model/kubernetes/relationship/Edge
// Relationships can be further filtered through query parameter
// ?version={version}
// ?order={field} orders on the passed field
// ?sort={[asc/desc]} Default behavior is asc
// ?search={[true/false]} If search is true then a greedy search is performed
// ?page={page-number} Default page number is 1
// ?pagesize={pagesize} Default pagesize is 25. To return all results: pagesize=all
// responses:
// 200: []RelationshipDefinition
func (h *Handler) GetMeshmodelRelationshipByName(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Add("Content-Type", "application/json")
	enc := json.NewEncoder(rw)
	typ := mux.Vars(r)["model"]
	name := mux.Vars(r)["name"]
	var greedy bool
	if r.URL.Query().Get("search") == "true" {
		greedy = true
	}
	limitstr := r.URL.Query().Get("pagesize")
	var limit int
	if limitstr != "all" {
		limit, _ = strconv.Atoi(limitstr)
		if limit == 0 { //If limit is unspecified then it defaults to 25
			limit = DefaultPageSizeForMeshModelComponents
		}
	}
	pagestr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pagestr)
	if page == 0 {
		page = 1
	}
	offset := (page - 1) * limit
	res := h.registryManager.GetEntities(&v1alpha1.RelationshipFilter{
		Kind:      name,
		ModelName: typ,
		Greedy:    greedy,
		Limit:     limit,
		Offset:    offset,
		OrderOn:   r.URL.Query().Get("order"),
		Sort:      r.URL.Query().Get("sort"),
	})
	var rels []v1alpha1.RelationshipDefinition
	for _, r := range res {
		rel, ok := r.(v1alpha1.RelationshipDefinition)
		if ok {
			rels = append(rels, rel)
		}
	}
	if err := enc.Encode(rels); err != nil {
		h.log.Error(ErrWorkloadDefinition(err)) //TODO: Add appropriate meshkit error
		http.Error(rw, ErrWorkloadDefinition(err).Error(), http.StatusInternalServerError)
	}
}

// swagger:route GET /api/meshmodel/model/{model}/relationship GetAllMeshmodelRelationships idGetAllMeshmodelRelationships
// Handle GET request for getting meshmodel relationships of a specific model.
// Example: /api/meshmodel/model/kubernetes/relationship
// Relationships can be further filtered through query parameter
// ?version={version}
// ?order={field} orders on the passed field
// ?sort={[asc/desc]} Default behavior is asc
// ?page={page-number} Default page number is 1
// ?pagesize={pagesize} Default pagesize is 25. To return all results: pagesize=all
// responses:
// 200: []RelationshipDefinition
func (h *Handler) GetAllMeshmodelRelationships(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Add("Content-Type", "application/json")
	enc := json.NewEncoder(rw)
	typ := mux.Vars(r)["model"]
	limitstr := r.URL.Query().Get("pagesize")
	var limit int
	if limitstr != "all" {
		limit, _ = strconv.Atoi(limitstr)
		if limit == 0 { //If limit is unspecified then it defaults to 25
			limit = DefaultPageSizeForMeshModelComponents
		}
	}
	pagestr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pagestr)
	if page == 0 {
		page = 1
	}
	offset := (page - 1) * limit
	res := h.registryManager.GetEntities(&v1alpha1.RelationshipFilter{
		Version:   r.URL.Query().Get("version"),
		ModelName: typ,
		Limit:     limit,
		Offset:    offset,
		OrderOn:   r.URL.Query().Get("order"),
		Sort:      r.URL.Query().Get("sort"),
	})
	var rels []v1alpha1.RelationshipDefinition
	for _, r := range res {
		rel, ok := r.(v1alpha1.RelationshipDefinition)
		if ok {
			rels = append(rels, rel)
		}
	}
	if err := enc.Encode(rels); err != nil {
		h.log.Error(ErrWorkloadDefinition(err)) //TODO: Add appropriate meshkit error
		http.Error(rw, ErrWorkloadDefinition(err).Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) RegisterMeshmodelRelationships(rw http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	var cc meshmodel.MeshModelRegistrantData
	err := dec.Decode(&cc)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	switch cc.EntityType {
	case types.RelationshipDefinition:
		var r v1alpha1.RelationshipDefinition
		err = json.Unmarshal(cc.Entity, &r)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		err = h.registryManager.RegisterEntity(cc.Host, r)
	}
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	go h.config.MeshModelSummaryChannel.Publish()
}

// while parsing, if an error is encountered, it will return the list of relationships that have already been parsed along with the error
func parseStaticRelationships(sourceDirPath string) (rs []v1alpha1.RelationshipDefinition, err error) {
	err = filepath.Walk(sourceDirPath, func(path string, info fs.FileInfo, err error) error {
		if info == nil {
			return fmt.Errorf("invalid/nil fileinfo while walking %s", path)
		}
		if !info.IsDir() {
			var rel v1alpha1.RelationshipDefinition
			byt, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			err = json.Unmarshal(byt, &rel)
			if err != nil {
				return err
			}
			rs = append(rs, rel)
		}
		return nil
	})
	return
}

func RegisterStaticMeshmodelRelationships(rm meshmodel.RegistryManager, sourceDirPath string) (err error) {
	host := meshmodel.Host{Hostname: "meshery"}
	rs, err := parseStaticRelationships(path.Clean(sourceDirPath))
	if err != nil && len(rs) == 0 {
		return
	}
	for _, r := range rs {
		err = rm.RegisterEntity(host, r)
		if err != nil {
			return
		}
	}
	return
}