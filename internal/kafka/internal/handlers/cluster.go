package handlers

import (
	"net/http"

	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/api/public"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/clusters"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/presenters"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/services"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/api"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/errors"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/handlers"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/shared"
	"github.com/gorilla/mux"
)

type clusterHandler struct {
	kasFleetshardOperatorAddon services.KasFleetshardOperatorAddon
	clusterService             services.ClusterService
	providerFactory            clusters.ProviderFactory
}

func NewClusterHandler(kasFleetshardOperatorAddon services.KasFleetshardOperatorAddon, clusterService services.ClusterService, providerFactory clusters.ProviderFactory) *clusterHandler {
	return &clusterHandler{
		kasFleetshardOperatorAddon: kasFleetshardOperatorAddon,
		clusterService:             clusterService,
		providerFactory:            providerFactory,
	}
}

func (h clusterHandler) RegisterEnterpriseCluster(w http.ResponseWriter, r *http.Request) {
	var clusterPayload public.EnterpriseOsdClusterPayload

	ctx := r.Context()

	provider, err := h.providerFactory.GetProvider(api.ClusterProviderOCM)

	cfg := &handlers.HandlerConfig{
		MarshalInto: &clusterPayload,
		Validate: []handlers.Validate{
			h.validateOCMProviderAvailable(provider, err),

			handlers.ValidateLength(&clusterPayload.ClusterId, "cluster id", ClusterIdLength, &ClusterIdLength),

			handlers.ValidateNotEmptyClusterId(&clusterPayload.ClusterId, "cluster id"),

			ValidateClusterIdIsUnique(&clusterPayload.ClusterId, h.clusterService),

			handlers.ValidateDnsName(&clusterPayload.ClusterIngressDnsName, "cluster dns name"),

			validateKafkaMachinePoolNodeCount(&clusterPayload),
		},
		Action: func() (interface{}, *errors.ServiceError) {

			clusterSpec, getClusterErr := provider.GetClusterSpec(clusterPayload.ClusterId)
			if getClusterErr != nil && shared.IsNil(clusterSpec) {
				return nil, errors.GeneralError("failed to get cluster by ID: %s", clusterPayload.ClusterId)
			}

			if !clusterSpec.MultiAZ {
				return nil, errors.BadRequest("single AZ clusters are not supported")
			}

			if !shared.StringEqualsIgnoreCase(clusterSpec.Status.String(), api.ClusterProvisioned.String()) {
				return nil, errors.BadRequest("cluster that are not yet fully provisioned are not accepted")
			}

			claims, claimsErr := getClaims(ctx)

			if claimsErr != nil {
				return nil, claimsErr
			}

			// TODO - validate that the org also owns the cluster.
			orgId, getOrgIdErr := claims.GetOrgId()
			if getOrgIdErr != nil {
				return nil, errors.GeneralError(getOrgIdErr.Error())
			}

			if !claims.IsOrgAdmin() {
				return nil, errors.New(errors.ErrorUnauthorized, "non admin user not authorized to perform this action")
			}

			supportedKafkaInstanceType := api.StandardTypeSupport.String()
			clusterRequest := &api.Cluster{
				ClusterType:                   api.EnterpriseDataPlaneClusterType.String(),
				ProviderType:                  api.ClusterProviderOCM,
				Status:                        api.ClusterAccepted,
				CloudProvider:                 clusterSpec.CloudProvider,
				Region:                        clusterSpec.Region,
				ClusterID:                     clusterPayload.ClusterId,
				OrganizationID:                orgId,
				ClusterDNS:                    clusterPayload.ClusterIngressDnsName,
				ExternalID:                    clusterSpec.ExternalID,
				MultiAZ:                       clusterSpec.MultiAZ,
				AccessKafkasViaPrivateNetwork: clusterPayload.AccessKafkasViaPrivateNetwork,
				SupportedInstanceType:         supportedKafkaInstanceType,
			}

			capacityInfo := map[string]api.DynamicCapacityInfo{
				supportedKafkaInstanceType: {
					MaxNodes: clusterPayload.KafkaMachinePoolNodeCount,
				},
			}

			err := clusterRequest.SetDynamicCapacityInfo(capacityInfo)
			if err != nil { // this should never occur
				return nil, errors.GeneralError("invalid node count info")
			}

			fsoParams, svcErr := h.kasFleetshardOperatorAddon.GetAddonParams(clusterRequest)

			if svcErr != nil {
				return nil, svcErr
			}

			clusterRequest.ClientID = fsoParams.GetParam(services.KasFleetshardOperatorParamServiceAccountId)
			clusterRequest.ClientSecret = fsoParams.GetParam(services.KasFleetshardOperatorParamServiceAccountSecret)

			svcErr = h.clusterService.RegisterClusterJob(clusterRequest)
			if svcErr != nil {
				return nil, svcErr
			}
			return presenters.PresentEnterpriseClusterWithAddonParams(*clusterRequest, fsoParams)
		},
	}

	// return 200 status ok
	handlers.Handle(w, r, cfg, http.StatusOK)
}

func (h clusterHandler) validateOCMProviderAvailable(provider clusters.Provider, err error) handlers.Validate {
	return func() *errors.ServiceError {
		if err != nil || shared.IsNil(provider) {
			return errors.GeneralError("unexpected error occurred. failed to validate the request")
		}
		return nil
	}
}

func (h clusterHandler) List(w http.ResponseWriter, r *http.Request) {
	cfg := &handlers.HandlerConfig{
		Action: func() (interface{}, *errors.ServiceError) {
			ctx := r.Context()

			clusters, err := h.clusterService.ListEnterpriseClustersOfAnOrganization(ctx)
			if err != nil {
				return nil, err
			}

			clusterList := public.EnterpriseClusterList{
				Kind:  "ClusterList",
				Page:  1,
				Size:  int32(len(clusters)),
				Total: int32(len(clusters)),
				Items: []public.EnterpriseCluster{},
			}

			for _, cluster := range clusters {
				converted := presenters.PresentEnterpriseCluster(*cluster)
				clusterList.Items = append(clusterList.Items, converted)
			}

			return clusterList, nil
		},
	}

	handlers.HandleList(w, r, cfg)
}

func (h clusterHandler) DeregisterEnterpriseCluster(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clusterID := mux.Vars(r)["id"]

	cfg := &handlers.HandlerConfig{
		Validate: []handlers.Validate{
			handlers.ValidateAsyncEnabled(r, "deleting enterprise cluster"),
			ValidateKafkaClaims(ctx, ValidateOrganisationId()),
			validateEnterpriseClusterEligibleForDeregistration(ctx, clusterID, h.clusterService),
			validateEnterpriseClusterHasNoKafkas(clusterID, h.clusterService),
		},
		Action: func() (i interface{}, serviceError *errors.ServiceError) {
			return nil, h.clusterService.DeregisterClusterJob(clusterID)
		},
	}
	handlers.HandleDelete(w, r, cfg, http.StatusAccepted)
}

func (h clusterHandler) Get(w http.ResponseWriter, r *http.Request) {
	clusterID := mux.Vars(r)["id"]
	ctx := r.Context()
	cfg := &handlers.HandlerConfig{
		Validate: []handlers.Validate{
			handlers.ValidateNotEmptyClusterId(&clusterID, "cluster id"),
			ValidateKafkaClaims(ctx, ValidateOrganisationId()),
		},
		Action: func() (i interface{}, serviceError *errors.ServiceError) {
			// error checked in the validate, no need to check again
			claims, _ := getClaims(ctx)
			orgID, _ := claims.GetOrgId()

			cluster, err := h.clusterService.FindClusterByID(clusterID)
			if err != nil {
				return nil, err
			}

			if cluster == nil || cluster.OrganizationID != orgID || cluster.ClusterType != api.EnterpriseDataPlaneClusterType.String() {
				return nil, errors.NotFound("enterprise data plane cluster with id='%v' not found within organization: %s", clusterID, orgID)
			}

			return presenters.PresentEnterpriseCluster(*cluster), nil
		},
	}
	handlers.HandleGet(w, r, cfg)
}

func (h clusterHandler) GetEnterpriseClusterWithAddonParams(w http.ResponseWriter, r *http.Request) {
	clusterID := mux.Vars(r)["id"]
	ctx := r.Context()
	cfg := &handlers.HandlerConfig{
		Validate: []handlers.Validate{
			handlers.ValidateNotEmptyClusterId(&clusterID, "cluster id"),
			ValidateKafkaClaims(ctx, ValidateOrganisationId()),
		},
		Action: func() (i interface{}, serviceError *errors.ServiceError) {
			// error checked in the validate, no need to check again
			claims, _ := getClaims(ctx)
			orgID, _ := claims.GetOrgId()

			if !claims.IsOrgAdmin() {
				return nil, errors.New(errors.ErrorUnauthorized, "non admin user not authorized to perform this action")
			}

			cluster, err := h.clusterService.FindClusterByID(clusterID)
			if err != nil {
				return nil, err
			}

			if cluster == nil || cluster.OrganizationID != orgID || cluster.ClusterType != api.EnterpriseDataPlaneClusterType.String() {
				return nil, errors.NotFound("enterprise data plane cluster with id='%v' not found within organization: %s", clusterID, orgID)
			}

			fsoParams, svcErr := h.kasFleetshardOperatorAddon.GetAddonParams(cluster)

			if svcErr != nil {
				return nil, svcErr
			}

			return presenters.PresentEnterpriseClusterWithAddonParams(*cluster, fsoParams)
		},
	}
	handlers.HandleGet(w, r, cfg)
}
