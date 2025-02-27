package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/api/public"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/clusters"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/clusters/types"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/services"
	mocks "github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/test/mocks/kafkas"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/api"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/auth"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/errors"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/mux"
	"github.com/onsi/gomega"
)

const (
	invalidParam                 = "invalid"
	validLengthClusterId         = "1234abcd1234abcd1234abcd1234abcd"
	validFormatExternalClusterId = "69d631de-9b7f-4bc2-bf4f-4d3295a7b25e"
	validDnsName                 = "apps.enterprise-aws.awdk.s1.devshift.org"
)

var (
	ctxWithClaims = auth.SetTokenInContext(context.TODO(), &jwt.Token{
		Claims: jwt.MapClaims{
			"username":     "test-user",
			"org_id":       mocks.DefaultOrganisationId,
			"is_org_admin": true,
		},
	})
	nonAdminCtxWithClaims = auth.SetTokenInContext(context.TODO(), &jwt.Token{
		Claims: jwt.MapClaims{
			"username":     "non-admin-user",
			"org_id":       mocks.DefaultOrganisationId,
			"is_org_admin": false,
		},
	})
	entClusterID = "1234abcd1234abcd1234abcd1234abcd"
)

func Test_RegisterEnterpriseCluster(t *testing.T) {
	g := gomega.NewWithT(t)
	type fields struct {
		kasFleetshardOperatorAddon services.KasFleetshardOperatorAddon
		clusterService             services.ClusterService
		providerFactory            clusters.ProviderFactory
	}

	type args struct {
		body []byte
		ctx  context.Context
	}

	tests := []struct {
		name           string
		fields         fields
		args           args
		wantStatusCode int
		want           *public.EnterpriseClusterWithAddonParameters
	}{
		{
			name: "should return an error if body is empty",
			args: args{
				body: []byte(`{}`),
				ctx:  context.TODO(),
			},
			fields: fields{
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if cluster_id is invalid",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s"}`, invalidParam)),
				ctx:  context.TODO(),
			},
			fields: fields{
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if FindClusterByID returns error other than cluster not found",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": false}`, validLengthClusterId)),
				ctx:  context.TODO(),
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, errors.GeneralError("unexpected error")
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should return an error if cluster_ingress_dns_name is empty",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": false}`, validLengthClusterId)),
				ctx:  context.TODO(),
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if cluster_dns_name is invalid",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": true, "cluster_ingress_dns_name": "%s"}`, validLengthClusterId, invalidParam)),
				ctx:  context.TODO(),
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if kafka_machine_pool_node_count is less than 3",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": false, "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 2}`, validLengthClusterId, validDnsName)),
				ctx:  context.TODO(),
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if kafka_machine_pool_node_count is not a multiple of 3",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 5}`, validLengthClusterId, validDnsName)),
				ctx:  context.TODO(),
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if GetProvider fails",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 6}`, validLengthClusterId, validDnsName)),
				ctx:  context.TODO(),
			},
			fields: fields{
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return nil, errors.GeneralError("failed to get ocm provider")
					},
				},
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should return an error if cluster is single AZ",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 6}`, validLengthClusterId, validDnsName)),
				ctx:  context.TODO(),
			},
			fields: fields{
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{
									MultiAZ: false,
									Status:  api.ClusterProvisioned,
								}, nil
							},
						}, nil
					},
				},
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if cluster is not in cluster_provisioned state",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 6}`, validLengthClusterId, validDnsName)),
				ctx:  ctxWithClaims,
			},
			fields: fields{
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{
									MultiAZ: true,
									Status:  api.ClusterAccepted,
								}, nil
							},
						}, nil
					},
				},
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "should return an error if claims cant be obtained from context",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": false, "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 3}`, validLengthClusterId, validDnsName)),
				ctx:  context.TODO(),
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{
									MultiAZ:    true,
									InternalID: validLengthClusterId,
									Status:     api.ClusterProvisioned,
								}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should return an error if GetAddonParams returns an error",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": true, "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 3}`, validLengthClusterId, validDnsName)),
				ctx:  ctxWithClaims,
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
				kasFleetshardOperatorAddon: &services.KasFleetshardOperatorAddonMock{
					GetAddonParamsFunc: func(cluster *api.Cluster) (services.ParameterList, *errors.ServiceError) {
						return nil, errors.GeneralError("failed to get addons")
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{
									MultiAZ:    true,
									InternalID: validLengthClusterId,
									Status:     api.ClusterProvisioned,
								}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should return an error if RegisterClusterJob returns an error",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": false, "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 3}`, validLengthClusterId, validDnsName)),
				ctx:  ctxWithClaims,
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
					RegisterClusterJobFunc: func(clusterRequest *api.Cluster) *errors.ServiceError {
						return errors.GeneralError("failed to register cluster")
					},
				},
				kasFleetshardOperatorAddon: &services.KasFleetshardOperatorAddonMock{
					GetAddonParamsFunc: func(cluster *api.Cluster) (services.ParameterList, *errors.ServiceError) {
						return services.ParameterList{
							{
								Id:    "some-id",
								Value: "value",
							},
						}, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{
									MultiAZ:    true,
									InternalID: validLengthClusterId,
									Status:     api.ClusterProvisioned,
								}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should successfully register enterprise cluster",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": false, "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 3}`, validLengthClusterId, validDnsName)),
				ctx:  ctxWithClaims,
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
					RegisterClusterJobFunc: func(clusterRequest *api.Cluster) *errors.ServiceError {
						return nil
					},
				},
				kasFleetshardOperatorAddon: &services.KasFleetshardOperatorAddonMock{
					GetAddonParamsFunc: func(cluster *api.Cluster) (services.ParameterList, *errors.ServiceError) {
						return services.ParameterList{
							{
								Id:    "some-id",
								Value: "value",
							},
						}, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{
									MultiAZ:       true,
									InternalID:    validLengthClusterId,
									Region:        mocks.DefaultKafkaRequestRegion,
									CloudProvider: "aws",
									Status:        api.ClusterProvisioned,
								}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusOK,
			want: &public.EnterpriseClusterWithAddonParameters{
				Status:                        api.ClusterAccepted.String(),
				ClusterId:                     validLengthClusterId,
				Id:                            validLengthClusterId,
				AccessKafkasViaPrivateNetwork: false,
				Kind:                          "Cluster",
				Href:                          fmt.Sprintf("/api/kafkas_mgmt/v1/clusters/%s", validLengthClusterId),
				FleetshardParameters: []public.FleetshardParameter{
					{
						Id:    "some-id",
						Value: "value",
					},
				},
			},
		},
		{
			name: "should successfully register enterprise cluster if FindClusterByID returns cluster not found error",
			args: args{
				body: []byte(fmt.Sprintf(`{"cluster_id": "%s", "access_kafkas_via_private_network": true, "cluster_ingress_dns_name": "%s", "kafka_machine_pool_node_count": 3}`, validLengthClusterId, validDnsName)),
				ctx:  ctxWithClaims,
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, errors.GeneralError("failed to find cluster")
					},
					RegisterClusterJobFunc: func(clusterRequest *api.Cluster) *errors.ServiceError {
						g.Expect(clusterRequest.MultiAZ).To(gomega.BeTrue())
						g.Expect(clusterRequest.ClusterID).To(gomega.Equal(validLengthClusterId))
						g.Expect(clusterRequest.ExternalID).To(gomega.Equal(validFormatExternalClusterId))
						g.Expect(clusterRequest.CloudProvider).To(gomega.Equal(mocks.DefaultKafkaRequestProvider))
						g.Expect(clusterRequest.Region).To(gomega.Equal(mocks.DefaultKafkaRequestRegion))
						return nil
					},
				},
				kasFleetshardOperatorAddon: &services.KasFleetshardOperatorAddonMock{
					GetAddonParamsFunc: func(cluster *api.Cluster) (services.ParameterList, *errors.ServiceError) {
						return services.ParameterList{
							{
								Id:    "some-id",
								Value: "value",
							},
						}, nil
					},
				},
				providerFactory: &clusters.ProviderFactoryMock{
					GetProviderFunc: func(providerType api.ClusterProviderType) (clusters.Provider, error) {
						return &clusters.ProviderMock{
							GetClusterSpecFunc: func(clusterID string) (types.ClusterSpec, error) {
								return types.ClusterSpec{
									MultiAZ:       true,
									InternalID:    validLengthClusterId,
									Region:        mocks.DefaultKafkaRequestRegion,
									ExternalID:    validFormatExternalClusterId,
									CloudProvider: mocks.DefaultKafkaRequestProvider,
									Status:        api.ClusterProvisioned,
								}, nil
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusOK,
			want: &public.EnterpriseClusterWithAddonParameters{
				Status:                        api.ClusterAccepted.String(),
				ClusterId:                     validLengthClusterId,
				Id:                            validLengthClusterId,
				AccessKafkasViaPrivateNetwork: true,
				Kind:                          "Cluster",
				Href:                          fmt.Sprintf("/api/kafkas_mgmt/v1/clusters/%s", validLengthClusterId),
				FleetshardParameters: []public.FleetshardParameter{
					{
						Id:    "some-id",
						Value: "value",
					},
				},
			},
		},
	}

	for _, testcase := range tests {
		tt := testcase
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			h := NewClusterHandler(tt.fields.kasFleetshardOperatorAddon, tt.fields.clusterService, tt.fields.providerFactory)
			req, rw := GetHandlerParams("POST", "", bytes.NewBuffer(tt.args.body), t)
			req = req.WithContext(tt.args.ctx)
			h.RegisterEnterpriseCluster(rw, req)
			resp := rw.Result()
			defer resp.Body.Close()
			g.Expect(resp.StatusCode).To(gomega.Equal(tt.wantStatusCode))
			if tt.wantStatusCode == http.StatusOK {
				cluster := &public.EnterpriseClusterWithAddonParameters{}
				err := json.NewDecoder(resp.Body).Decode(&cluster)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(cluster).To(gomega.Equal(tt.want))
			}
		})
	}
}

func Test_ListEnterpriseClusters(t *testing.T) {
	type fields struct {
		clusterService services.ClusterService
	}

	type args struct {
		ctx context.Context
	}

	tests := []struct {
		name           string
		fields         fields
		args           args
		wantStatusCode int
		want           public.EnterpriseClusterList
	}{

		{
			name: "should return an error if List returns an error",
			args: args{
				ctx: ctxWithClaims,
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					ListEnterpriseClustersOfAnOrganizationFunc: func(ctx context.Context) ([]*api.Cluster, *errors.ServiceError) {
						return nil, errors.GeneralError("failed to register cluster")
					},
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should successfully List enterprise clusters",
			args: args{
				ctx: ctxWithClaims,
			},
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					ListEnterpriseClustersOfAnOrganizationFunc: func(ctx context.Context) ([]*api.Cluster, *errors.ServiceError) {
						return []*api.Cluster{
							{
								ClusterID: validLengthClusterId,
								Status:    api.ClusterReady,
							},
						}, nil
					},
				},
			},
			wantStatusCode: http.StatusOK,
			want: public.EnterpriseClusterList{
				Kind:  "ClusterList",
				Page:  1,
				Size:  int32(1),
				Total: int32(1),
				Items: []public.EnterpriseCluster{
					{
						Status:    api.ClusterReady.String(),
						ClusterId: validLengthClusterId,
						Id:        validLengthClusterId,
						Kind:      "Cluster",
						Href:      fmt.Sprintf("/api/kafkas_mgmt/v1/clusters/%s", validLengthClusterId),
					},
				},
			},
		},
	}

	for _, testcase := range tests {
		tt := testcase
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			h := NewClusterHandler(nil, tt.fields.clusterService, nil)
			req, rw := GetHandlerParams("GET", "", nil, t)
			req = req.WithContext(tt.args.ctx)
			h.List(rw, req)
			resp := rw.Result()
			defer resp.Body.Close()
			g.Expect(resp.StatusCode).To(gomega.Equal(tt.wantStatusCode))
			if tt.wantStatusCode == http.StatusOK {
				clusterList := public.EnterpriseClusterList{}
				err := json.NewDecoder(resp.Body).Decode(&clusterList)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(clusterList).To(gomega.Equal(tt.want))
			}
		})
	}
}

func Test_DeregisterEnterpriseCluster(t *testing.T) {
	type fields struct {
		clusterService services.ClusterService
	}

	type args struct {
		ctx         context.Context
		queryParams map[string]string
	}

	tests := []struct {
		name           string
		fields         fields
		args           args
		wantStatusCode int
	}{
		{
			name: "should fail if async is not set",
			args: args{
				ctx: context.TODO(),
			},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:   "should fail if organization ID is not available within context",
			fields: fields{},
			args: args{
				ctx: context.TODO(),
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name: "should fail if attempt to find cluster with provided clusterID returns an error",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, errors.GeneralError("failed to find cluster")
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should fail if cluster to deregister cannot be found",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "should fail if organizationID of cluster to deregister is different than one from context",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: "12345678",
						}, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name: "should fail if cluster_type of cluster to deregister is not 'enterprise'",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    "non-enterprise",
						}, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name: "should successfully trigger deregistration of a cluster when all other preconditions are met",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
							ClusterID:      entClusterID,
						}, nil
					},
					FindKafkaInstanceCountFunc: func(clusterIDs []string) ([]services.ResKafkaInstanceCount, error) {
						return []services.ResKafkaInstanceCount{
							{
								Clusterid: entClusterID,
								Count:     0,
							},
						}, nil
					},
					DeregisterClusterJobFunc: func(clusterID string) *errors.ServiceError {
						return nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusAccepted,
		},
		{
			name: "should fail when kafka requests are present on cluster to be deregistered",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
							ClusterID:      entClusterID,
						}, nil
					},
					FindKafkaInstanceCountFunc: func(clusterIDs []string) ([]services.ResKafkaInstanceCount, error) {
						return []services.ResKafkaInstanceCount{
							{
								Clusterid: entClusterID,
								Count:     1,
							},
						}, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name: "should fail when FindKafkaInstanceCount returns an error",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
							ClusterID:      entClusterID,
						}, nil
					},
					FindKafkaInstanceCountFunc: func(clusterIDs []string) ([]services.ResKafkaInstanceCount, error) {
						return nil, errors.GeneralError("failed to get kafka instance count")
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should successfully trigger cluster deregistration when no kafka requests are present on the cluster",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
							ClusterID:      entClusterID,
						}, nil
					},
					FindKafkaInstanceCountFunc: func(clusterIDs []string) ([]services.ResKafkaInstanceCount, error) {
						return nil, nil
					},
					DeregisterClusterJobFunc: func(clusterID string) *errors.ServiceError {
						return nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
				queryParams: map[string]string{
					"async": "true",
				},
			},
			wantStatusCode: http.StatusAccepted,
		},
	}

	for _, testcase := range tests {
		tt := testcase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := gomega.NewWithT(t)
			h := NewClusterHandler(nil, tt.fields.clusterService, nil)
			req, rw := GetHandlerParams("DELETE", "/{id}", nil, t)
			if tt.args.queryParams != nil {
				q := req.URL.Query()
				for k, v := range tt.args.queryParams {
					q.Add(k, v)
				}
				req.URL.RawQuery = q.Encode()
			}
			req = req.WithContext(tt.args.ctx)
			req = mux.SetURLVars(req, map[string]string{"id": entClusterID})
			h.DeregisterEnterpriseCluster(rw, req)
			resp := rw.Result()
			resp.Body.Close()
			g.Expect(resp.StatusCode).To(gomega.Equal(tt.wantStatusCode))
		})
	}
}

func Test_GetEnterpriseCluster(t *testing.T) {
	type fields struct {
		clusterService services.ClusterService
	}

	type args struct {
		ctx context.Context
	}

	tests := []struct {
		name           string
		fields         fields
		args           args
		wantStatusCode int
		want           public.EnterpriseCluster
	}{
		{
			name:   "should fail if organization ID is not available within context",
			fields: fields{},
			args: args{
				ctx: context.TODO(),
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name: "should fail if attempt to find cluster with provided clusterID returns an error",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, errors.GeneralError("failed to find cluster")
					},
				},
			},
			args: args{
				ctx: nonAdminCtxWithClaims,
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should fail if cluster with given ID cannot be found",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
			},
			args: args{
				ctx: nonAdminCtxWithClaims,
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "should not return found cluster if its from different organization ID",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: "98765432",
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
						}, nil
					},
				},
			},
			args: args{
				ctx: nonAdminCtxWithClaims,
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "should not return found cluster if its not an enterprise cluster",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.ManagedDataPlaneClusterType.String(),
						}, nil
					},
				},
			},
			args: args{
				ctx: nonAdminCtxWithClaims,
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "should successfully return enterprise cluster for a user of the same org",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
							ClusterID:      entClusterID,
						}, nil
					},
				},
			},
			args: args{
				ctx: nonAdminCtxWithClaims,
			},
			wantStatusCode: http.StatusOK,
			want: public.EnterpriseCluster{
				ClusterId: entClusterID,
				Id:        entClusterID,
				Kind:      "Cluster",
				Href:      fmt.Sprintf("/api/kafkas_mgmt/v1/clusters/%s", entClusterID),
			},
		},
	}

	for _, testcase := range tests {
		tt := testcase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := gomega.NewWithT(t)
			h := NewClusterHandler(nil, tt.fields.clusterService, nil)
			req, rw := GetHandlerParams("GET", "/{id}", nil, t)
			req = mux.SetURLVars(req, map[string]string{"id": entClusterID})
			req = req.WithContext(tt.args.ctx)
			h.Get(rw, req)
			resp := rw.Result()
			if tt.wantStatusCode == http.StatusOK {
				cluster := public.EnterpriseCluster{}
				err := json.NewDecoder(resp.Body).Decode(&cluster)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(cluster).To(gomega.Equal(tt.want))
			}
			resp.Body.Close()
			g.Expect(resp.StatusCode).To(gomega.Equal(tt.wantStatusCode))
		})
	}
}

func Test_GetEnterpriseClusterWithAddonParams(t *testing.T) {
	type fields struct {
		clusterService             services.ClusterService
		kasFleetshardOperatorAddon services.KasFleetshardOperatorAddon
	}

	type args struct {
		ctx context.Context
	}

	tests := []struct {
		name           string
		fields         fields
		args           args
		wantStatusCode int
		want           public.EnterpriseClusterWithAddonParameters
	}{
		{
			name:   "should fail if organization ID is not available within context",
			fields: fields{},
			args: args{
				ctx: context.TODO(),
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name:   "should fail if attempting to hit the endpoint with non-admin user",
			fields: fields{},
			args: args{
				ctx: nonAdminCtxWithClaims,
			},
			wantStatusCode: http.StatusForbidden,
		},
		{
			name: "should fail if attempt to find cluster with provided clusterID returns an error",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, errors.GeneralError("failed to find cluster")
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should fail if cluster with given ID cannot be found",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return nil, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "should not return found cluster if its from different organization ID",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: "98765432",
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
						}, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "should not return found cluster if its not an enterprise cluster",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.ManagedDataPlaneClusterType.String(),
						}, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name: "should fail if GetAddonParams returns an error",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
						}, nil
					},
				},
				kasFleetshardOperatorAddon: &services.KasFleetshardOperatorAddonMock{
					GetAddonParamsFunc: func(cluster *api.Cluster) (services.ParameterList, *errors.ServiceError) {
						return nil, errors.GeneralError("failed to get addons")
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
			},
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "should successfully return enterprise cluster for admin user of the same org",
			fields: fields{
				clusterService: &services.ClusterServiceMock{
					FindClusterByIDFunc: func(clusterID string) (*api.Cluster, *errors.ServiceError) {
						return &api.Cluster{
							ClusterID:      entClusterID,
							OrganizationID: mocks.DefaultOrganisationId,
							ClusterType:    api.EnterpriseDataPlaneClusterType.String(),
						}, nil
					},
				},
				kasFleetshardOperatorAddon: &services.KasFleetshardOperatorAddonMock{
					GetAddonParamsFunc: func(cluster *api.Cluster) (services.ParameterList, *errors.ServiceError) {
						return services.ParameterList{
							{
								Id:    "some-id",
								Value: "value",
							},
						}, nil
					},
				},
			},
			args: args{
				ctx: ctxWithClaims,
			},
			wantStatusCode: http.StatusOK,
			want: public.EnterpriseClusterWithAddonParameters{
				ClusterId: entClusterID,
				Id:        entClusterID,
				Kind:      "Cluster",
				Href:      fmt.Sprintf("/api/kafkas_mgmt/v1/clusters/%s", entClusterID),
				FleetshardParameters: []public.FleetshardParameter{
					{
						Id:    "some-id",
						Value: "value",
					},
				},
			},
		},
	}

	for _, testcase := range tests {
		tt := testcase
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := gomega.NewWithT(t)
			h := NewClusterHandler(tt.fields.kasFleetshardOperatorAddon, tt.fields.clusterService, nil)
			req, rw := GetHandlerParams("GET", "/{id}/addon_parameters", nil, t)
			req = mux.SetURLVars(req, map[string]string{"id": entClusterID})
			req = req.WithContext(tt.args.ctx)
			h.GetEnterpriseClusterWithAddonParams(rw, req)
			resp := rw.Result()
			if tt.wantStatusCode == http.StatusOK {
				cluster := public.EnterpriseClusterWithAddonParameters{}
				err := json.NewDecoder(resp.Body).Decode(&cluster)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				g.Expect(cluster).To(gomega.Equal(tt.want))
			}
			resp.Body.Close()
			g.Expect(resp.StatusCode).To(gomega.Equal(tt.wantStatusCode))
		})
	}
}
