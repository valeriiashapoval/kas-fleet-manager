package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/shared/utils/arrays"

	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/shared"

	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/api"

	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/constants"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/api/admin/private"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/api/dbapi"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/api/public"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/config"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/kafkas/types"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/internal/kafka/internal/services"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/auth"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/errors"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/handlers"
	coreServices "github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/services"
	"github.com/bf2fc6cc711aee1a0c2a/kas-fleet-manager/pkg/services/authorization"
	resource "k8s.io/apimachinery/pkg/api/resource"
)

var ValidKafkaClusterNameRegexp = regexp.MustCompile(`^[a-z]([-a-z0-9]*[a-z0-9])?$`)

var MaxKafkaNameLength = 32

var ClusterIdLength = 32

const minimunNumberOfNodesForTheKafkaMachinePool = 3

func validateKafkaBillingModel(ctx context.Context, kafkaService services.KafkaService, kafkaConfig *config.KafkaConfig, kafkaRequestPayload *public.KafkaRequestPayload) handlers.Validate {
	return func() *errors.ServiceError {
		billingModel := shared.SafeString(kafkaRequestPayload.BillingModel)

		// No explicitly set kafka billing mode is allowed for now, in which case
		// an implementation-defined default is chosen
		if shared.StringEmpty(billingModel) {
			return nil
		}

		// enterprise kafkas billing model validation
		if !shared.StringEmpty(kafkaRequestPayload.ClusterId) && !shared.StringEqualsIgnoreCase(billingModel, constants.BillingModelEnterprise.String()) {
			return errors.InvalidBillingAccount("invalid billing model: %q, only %q is allowed", billingModel,
				constants.BillingModelEnterprise.String())
		}

		if shared.StringEmpty(kafkaRequestPayload.ClusterId) && shared.StringEqualsIgnoreCase(billingModel, constants.BillingModelEnterprise.String()) {
			return errors.BadRequest("cluster_id must be supplied when selected billing model is: %q",
				constants.BillingModelEnterprise.String())
		}

		instanceType, _, svcErr := getInstanceTypeAndSize(ctx, kafkaService, kafkaConfig, kafkaRequestPayload)
		if svcErr != nil {
			return svcErr
		}

		instanceTypeConfig, err := kafkaConfig.SupportedInstanceTypes.Configuration.GetKafkaInstanceTypeByID(instanceType)
		if err != nil {
			return errors.ToServiceError(err)
		}

		_, err = instanceTypeConfig.GetKafkaSupportedBillingModelByID(*kafkaRequestPayload.BillingModel)
		if err != nil {
			return errors.ToServiceError(err)
		}

		return nil
	}
}

func ValidateBillingCloudAccountIdAndMarketplace(ctx context.Context, kafkaService services.KafkaService, kafkaRequestPayload *public.KafkaRequestPayload) handlers.Validate {
	return func() *errors.ServiceError {
		// both fields are optional
		if shared.SafeString(kafkaRequestPayload.BillingCloudAccountId) == "" && shared.SafeString(kafkaRequestPayload.Marketplace) == "" {
			return nil
		}

		// marketplace without a billing account provided
		if shared.SafeString(kafkaRequestPayload.BillingCloudAccountId) == "" && shared.SafeString(kafkaRequestPayload.Marketplace) != "" {
			return errors.InvalidBillingAccount("no billing account provided for marketplace: %s", shared.SafeString(kafkaRequestPayload.Marketplace))
		}

		claims, err := getClaims(ctx)
		if err != nil {
			return err
		}

		owner, _ := claims.GetUsername()
		organisationId, _ := claims.GetOrgId()

		instanceType, err := kafkaService.AssignInstanceType(owner, organisationId)
		if err != nil {
			return errors.NewWithCause(errors.ErrorGeneral, err, "error assigning instance type: %s", err.Error())
		}

		return kafkaService.ValidateBillingAccount(organisationId, instanceType, shared.SafeString(kafkaRequestPayload.BillingModel), shared.SafeString(kafkaRequestPayload.BillingCloudAccountId), kafkaRequestPayload.Marketplace)
	}
}

func ValidKafkaClusterName(value *string, field string) handlers.Validate {
	return func() *errors.ServiceError {
		if !ValidKafkaClusterNameRegexp.MatchString(*value) {
			return errors.MalformedKafkaClusterName("%s does not match %s", field, ValidKafkaClusterNameRegexp.String())
		}
		return nil
	}
}

// ValidateKafkaClusterNameIsUnique returns a validator that validates that the kafka cluster name is unique
func ValidateKafkaClusterNameIsUnique(name *string, kafkaService services.KafkaService, context context.Context) handlers.Validate {
	return func() *errors.ServiceError {

		_, pageMeta, err := kafkaService.List(context, &coreServices.ListArguments{Page: 1, Size: 1, Search: fmt.Sprintf("name = %s", *name)})
		if err != nil {
			return err
		}

		if pageMeta.Total > 0 {
			return errors.DuplicateKafkaClusterName()
		}

		return nil
	}
}

func ValidateClusterIdIsUnique(clusterId *string, clusterService services.ClusterService) handlers.Validate {
	return func() *errors.ServiceError {

		cluster, err := clusterService.FindClusterByID(*clusterId)

		if err != nil && !strings.HasPrefix(err.Reason, "failed to find cluster") {
			return err
		}

		if cluster != nil {
			return errors.DuplicateClusterId()
		}

		return nil
	}
}

func validateVersionsCompatibility(h *adminKafkaHandler, kafkaRequest *dbapi.KafkaRequest, kafkaUpdateReq *private.KafkaUpdateRequest) handlers.Validate {
	return func() *errors.ServiceError { // Validate strimzi, kafka, and kafka IBP version
		desiredStrimziVersion := arrays.FirstNonEmptyOrDefault(kafkaRequest.DesiredStrimziVersion, kafkaUpdateReq.StrimziVersion)
		desiredKafkaVersion := arrays.FirstNonEmptyOrDefault(kafkaRequest.DesiredKafkaVersion, kafkaUpdateReq.KafkaVersion)
		desiredKafkaIBPVersion := arrays.FirstNonEmptyOrDefault(kafkaRequest.DesiredKafkaIBPVersion, kafkaUpdateReq.KafkaIbpVersion)

		cluster, err := h.clusterService.FindClusterByID(kafkaRequest.ClusterID)
		if err != nil {
			return errors.NewWithCause(errors.ErrorGeneral, err, "unable to find cluster associated with kafka request: %s", kafkaRequest.ID)
		}
		if cluster == nil {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to get cluster for kafka %s", kafkaRequest.ID))
		}

		if kafkaVersionAvailable, err := h.clusterService.IsStrimziKafkaVersionAvailableInCluster(cluster, desiredStrimziVersion, desiredKafkaVersion, desiredKafkaIBPVersion); err != nil {
			return errors.Validation(err.Error())
		} else if !kafkaVersionAvailable {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to update kafka: %s with kafka version: %s", kafkaRequest.ID, desiredKafkaVersion))
		}

		if strimziVersionReady, err := h.clusterService.CheckStrimziVersionReady(cluster, desiredStrimziVersion); err != nil {
			return errors.Validation(err.Error())
		} else if !strimziVersionReady {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to update kafka: %s with strimzi version: %s", kafkaRequest.ID, desiredStrimziVersion))
		}

		currentIBPVersion, _ := arrays.FirstNonEmpty(kafkaRequest.ActualKafkaIBPVersion, desiredKafkaIBPVersion)

		if vCompOldNewIbp, err := api.CompareBuildAwareSemanticVersions(currentIBPVersion, desiredKafkaIBPVersion); err != nil {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to compare actual ibp version: %s with desired ibp version: %s", currentIBPVersion, desiredKafkaIBPVersion))
		} else if vCompOldNewIbp > 0 {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to downgrade kafka: %s ibp version: %s to a lower version: %s", kafkaRequest.ID, desiredKafkaIBPVersion, currentIBPVersion))
		}

		if vCompIbpKafka, err := api.CompareBuildAwareSemanticVersions(desiredKafkaIBPVersion, desiredKafkaVersion); err != nil {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to compare kafka ibp version: %s with kafka version: %s", desiredKafkaIBPVersion, desiredKafkaVersion))
		} else if vCompIbpKafka > 0 {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to update kafka: %s ibp version: %s with kafka version: %s", kafkaRequest.ID, desiredKafkaIBPVersion, desiredKafkaVersion))
		}

		currentKafkaVersion, _ := arrays.FirstNonEmpty(kafkaRequest.ActualKafkaVersion, desiredKafkaVersion)

		if vCompKafka, err := api.CompareSemanticVersionsMajorAndMinor(currentKafkaVersion, desiredKafkaVersion); err != nil {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to compare desired kafka version: %s with actual kafka version: %s", desiredKafkaVersion, currentKafkaVersion))
		} else if vCompKafka > 0 {
			return errors.New(errors.ErrorValidation, fmt.Sprintf("unable to downgrade kafka: %s version: %s to the following kafka version: %s", kafkaRequest.ID, currentKafkaVersion, desiredKafkaVersion))
		}

		return nil
	}
}

func getCloudProviderAndRegion(
	ctx context.Context,
	kafkaService services.KafkaService,
	kafkaRequest *public.KafkaRequestPayload,
	providerConfig *config.ProviderConfig) (string, string, *errors.ServiceError) {

	// Set Cloud Provider default if not received in the request
	supportedProviders := providerConfig.ProvidersConfig.SupportedProviders

	defaultProvider, _ := supportedProviders.GetDefault()
	providerName := arrays.FirstNonEmptyOrDefault(defaultProvider.Name, kafkaRequest.CloudProvider)
	// Validation for Cloud Provider
	provider, providerSupported := supportedProviders.GetByName(providerName)

	// We only return a validation error if the provider is not supported and Kafka doesn't have a dedicated data plane cluster assigned to it
	if !providerSupported && shared.StringEmpty(kafkaRequest.ClusterId) {
		return "", "", errors.ProviderNotSupported("provider %s is not supported, supported providers are: %s", kafkaRequest.CloudProvider, supportedProviders)
	}

	// Validation for Cloud Region when the Kafka is not assignd to a dedicated data plane cluster
	if !shared.StringEmpty(kafkaRequest.Region) && shared.StringEmpty(kafkaRequest.ClusterId) { // if region is empty, default region will be chosen, so no validation is needed
		regionSupported := provider.IsRegionSupported(kafkaRequest.Region)
		if !regionSupported {
			return "", "", errors.RegionNotSupported("region %s is not supported for %s, supported regions are: %s", kafkaRequest.Region, kafkaRequest.CloudProvider, provider.Regions)
		}
	}

	claims, err := getClaims(ctx)
	if err != nil {
		return "", "", err
	}

	owner, _ := claims.GetUsername()
	organisationId, _ := claims.GetOrgId()

	// Validate Region/InstanceType
	instanceType, err := kafkaService.AssignInstanceType(owner, organisationId)
	if err != nil {
		return "", "", errors.NewWithCause(errors.ErrorGeneral, err, "error assigning instance type: %s", err.Error())
	}

	var region config.Region
	if kafkaRequest.Region == "" {
		region, _ = provider.GetDefaultRegion()
	} else {
		region, _ = provider.Regions.GetByName(kafkaRequest.Region)
	}

	// we only validate if the region supports the instance type when the Kafka is not assigned to a dedicated cluster
	if shared.StringEmpty(kafkaRequest.ClusterId) && !region.IsInstanceTypeSupported(config.InstanceType(instanceType)) {
		return "", "", errors.InstanceTypeNotSupported("instance type %q not supported for region %q", instanceType.String(), region.Name)
	}

	return providerName, region.Name, nil
}

// ValidateCloudProvider returns a validator that validates provided provider and region.
// The validation is only performed if the cluster id is not supplied in the given kafka request payload
// in this case the Kafka is an enterprise Kafka and we should not consider supported regions
func ValidateCloudProvider(ctx context.Context, kafkaService services.KafkaService, kafkaRequest *public.KafkaRequestPayload, providerConfig *config.ProviderConfig, action string) handlers.Validate {
	return func() *errors.ServiceError {
		_, _, err := getCloudProviderAndRegion(ctx, kafkaService, kafkaRequest, providerConfig)
		return err
	}
}

func getInstanceTypeAndSize(ctx context.Context, kafkaService services.KafkaService, kafkaConfig *config.KafkaConfig, kafkaRequestPayload *public.KafkaRequestPayload) (string, string, *errors.ServiceError) {
	claims, err := getClaims(ctx)
	if err != nil {
		return "", "", err
	}

	owner, _ := claims.GetUsername()
	organisationId, _ := claims.GetOrgId()
	instanceType, err := kafkaService.AssignInstanceType(owner, organisationId)
	if err != nil {
		return "", "", err
	}
	if stringSet(&kafkaRequestPayload.Plan) {
		plan := config.Plan(kafkaRequestPayload.Plan)
		instTypeFromPlan, err := plan.GetInstanceType()
		if err != nil || instTypeFromPlan != string(instanceType) {
			return "", "", errors.New(errors.ErrorBadRequest, fmt.Sprintf("unable to detect instance type in plan provided: %q", kafkaRequestPayload.Plan))
		}
		size, err := plan.GetSizeID()
		if err != nil {
			return "", "", errors.New(errors.ErrorBadRequest, fmt.Sprintf("unable to detect instance size in plan provided: %q", kafkaRequestPayload.Plan))
		}
		_, err = kafkaConfig.GetKafkaInstanceSize(instTypeFromPlan, size)

		if err != nil {
			return "", "", errors.InstancePlanNotSupported("unsupported plan provided: %q", kafkaRequestPayload.Plan)
		}
		return instanceType.String(), size, nil
	} else {
		rSize, err := kafkaConfig.GetFirstAvailableSize(instanceType.String())
		if err != nil {
			return "", "", errors.InstanceTypeNotSupported("unsupported kafka instance type: %q provided", instanceType.String())
		}
		return instanceType.String(), rSize.Id, nil
	}
}

// ValidateKafkaPlan - validate the requested Kafka Plan
func ValidateKafkaPlan(ctx context.Context, kafkaService services.KafkaService, kafkaConfig *config.KafkaConfig, kafkaRequestPayload *public.KafkaRequestPayload) handlers.Validate { // Validate plan
	return func() *errors.ServiceError {
		_, _, err := getInstanceTypeAndSize(ctx, kafkaService, kafkaConfig, kafkaRequestPayload)
		return err
	}
}

func ValidateKafkaUpdateFields(kafkaUpdateRequest *private.KafkaUpdateRequest) handlers.Validate {
	return func() *errors.ServiceError {
		if !(stringSet(&kafkaUpdateRequest.StrimziVersion) ||
			stringSet(&kafkaUpdateRequest.KafkaVersion) ||
			stringSet(&kafkaUpdateRequest.KafkaIbpVersion) ||
			stringSet(&kafkaUpdateRequest.DeprecatedKafkaStorageSize) ||
			stringSet(&kafkaUpdateRequest.MaxDataRetentionSize) ||
			shared.IsNotNil(kafkaUpdateRequest.Suspended)) {
			return errors.FieldValidationError("failed to update Kafka Request. Expecting at least one of the following fields: strimzi_version, kafka_version, kafka_ibp_version, kafka_storage_size, max_data_retention_size or suspended to be provided")
		}
		return nil
	}
}

func stringSet(value *string) bool {
	return value != nil && len(strings.Trim(*value, " ")) > 0
}

func validateUserIsKafkaOwnerOrOrgAdmin(ctx context.Context, kafkaRequest *dbapi.KafkaRequest) handlers.Validate {
	return func() *errors.ServiceError {
		claims, claimsErr := getClaims(ctx)
		if claimsErr != nil {
			return claimsErr
		}

		username, _ := claims.GetUsername()
		orgID, _ := claims.GetOrgId()
		isOrgAdmin := claims.IsOrgAdmin()

		authorized := kafkaRequest.OrganisationId == orgID && (isOrgAdmin || kafkaRequest.Owner == username)
		if !authorized {
			return errors.New(errors.ErrorUnauthorized, "user not authorized to perform this action")
		}
		return nil
	}
}

func ValidateKafkaUserFacingUpdateFields(ctx context.Context, authService authorization.Authorization, kafkaRequest *dbapi.KafkaRequest, kafkaUpdateReq *public.KafkaUpdateRequest) handlers.Validate {
	return func() *errors.ServiceError {
		claims, claimsErr := getClaims(ctx)
		if claimsErr != nil {
			return claimsErr
		}

		err := validateUserIsKafkaOwnerOrOrgAdmin(ctx, kafkaRequest)()
		if err != nil {
			return err
		}

		if kafkaUpdateReq.Owner != nil {
			validationError := handlers.ValidateMinLength(kafkaUpdateReq.Owner, "owner", 1)()
			if validationError != nil {
				return validationError
			}

			orgId, _ := claims.GetOrgId()
			userValid, err := authService.CheckUserValid(*kafkaUpdateReq.Owner, orgId)
			if err != nil {
				return errors.NewWithCause(errors.ErrorGeneral, err, "unable to update kafka request owner")
			}
			if !userValid {
				return errors.NewWithCause(errors.ErrorBadRequest, err, "user %s does not belong in your organization", *kafkaUpdateReq.Owner)
			}
		}

		return nil
	}
}

func getClaims(ctx context.Context) (auth.KFMClaims, *errors.ServiceError) {
	claims, err := auth.GetClaimsFromContext(ctx)
	if err != nil {
		return nil, errors.Unauthenticated("user not authenticated")
	}
	return auth.KFMClaims(claims), nil
}

type ValidateKafkaClaimsOptions func(claims *auth.KFMClaims) *errors.ServiceError

func ValidateUsername() ValidateKafkaClaimsOptions {
	return func(claims *auth.KFMClaims) *errors.ServiceError {
		if _, err := claims.GetUsername(); err != nil {
			return errors.New(errors.ErrorForbidden, err.Error())
		}
		return nil
	}
}

func ValidateOrganisationId() ValidateKafkaClaimsOptions {
	return func(claims *auth.KFMClaims) *errors.ServiceError {
		if _, err := claims.GetOrgId(); err != nil {
			return errors.New(errors.ErrorForbidden, err.Error())
		}
		return nil
	}
}

// ValidateKafkaClaims - Verifies that the context contains the required claims
func ValidateKafkaClaims(ctx context.Context, validations ...ValidateKafkaClaimsOptions) handlers.Validate {
	return func() *errors.ServiceError {
		claims, err := getClaims(ctx)

		for _, validation := range validations {
			if err = validation(&claims); err != nil {
				return err
			}
		}
		return err
	}
}

func ValidateKafkaStorageSize(kafkaRequest *dbapi.KafkaRequest, kafkaUpdateReq *private.KafkaUpdateRequest) handlers.Validate {
	return func() *errors.ServiceError {
		storageSize, _ := arrays.FirstNonEmpty(kafkaUpdateReq.MaxDataRetentionSize, kafkaUpdateReq.DeprecatedKafkaStorageSize)

		if stringSet(&storageSize) {
			currentSize, err := resource.ParseQuantity(kafkaRequest.KafkaStorageSize)
			if err != nil {
				return errors.FieldValidationError("failed to update Kafka Request. Unable to parse current storage size: %q", kafkaRequest.KafkaStorageSize)
			}
			requestedSize, err := resource.ParseQuantity(storageSize)
			if err != nil {
				return errors.FieldValidationError("failed to update Kafka Request. Unable to parse current requested size: %q", storageSize)
			}
			currSize, _ := currentSize.AsInt64()
			if requestedSize.CmpInt64(currSize) < 0 {
				return errors.FieldValidationError("failed to update Kafka Request. Requested size: %q should be greater than current size: %q", storageSize, kafkaRequest.KafkaStorageSize)
			}
		}
		return nil
	}
}

func validateKafkaMachinePoolNodeCount(clusterPayload *public.EnterpriseOsdClusterPayload) handlers.Validate {
	return func() *errors.ServiceError {
		if clusterPayload.KafkaMachinePoolNodeCount < minimunNumberOfNodesForTheKafkaMachinePool {
			return errors.FieldValidationError("failed to register cluster. Kafka machine pool node count: %d should be greater or equal to %d", clusterPayload.KafkaMachinePoolNodeCount, minimunNumberOfNodesForTheKafkaMachinePool)
		}

		remainder := clusterPayload.KafkaMachinePoolNodeCount % 3
		if remainder != 0 {
			return errors.FieldValidationError("failed to register cluster. Kafka machine pool node count: %d should be in multiple of 3", clusterPayload.KafkaMachinePoolNodeCount)
		}

		return nil
	}
}

func validateEnterpriseClusterEligibleForDeregistration(ctx context.Context, clusterID string, clusterService services.ClusterService) handlers.Validate {
	return func() *errors.ServiceError {
		claims, claimsErr := getClaims(ctx)

		if claimsErr != nil {
			return claimsErr
		}

		orgID, getOrgIdErr := claims.GetOrgId()

		if getOrgIdErr != nil {
			return errors.GeneralError(getOrgIdErr.Error())
		}

		if !claims.IsOrgAdmin() {
			return errors.New(errors.ErrorUnauthorized, "non admin user not authorized to perform this action")
		}

		cluster, err := clusterService.FindClusterByID(clusterID)
		if err != nil {
			return err
		}

		if cluster == nil {
			return errors.NotFound("cluster with id=%q not found", clusterID)
		}
		if cluster.OrganizationID != orgID {
			return errors.Forbidden("unable to deregister cluster from different organization")
		}
		if cluster.ClusterType != api.EnterpriseDataPlaneClusterType.String() {
			return errors.Forbidden("unable to deregister cluster whose type is not: %q", api.EnterpriseDataPlaneClusterType.String())
		}
		return nil
	}
}

// validateEnterpriseClusterHasNoKafkas requires a cluster to be empty, thus having no kafka instances
func validateEnterpriseClusterHasNoKafkas(clusterID string, clusterService services.ClusterService) handlers.Validate {
	return func() *errors.ServiceError {
		instanceCounts, err := clusterService.FindKafkaInstanceCount([]string{clusterID})
		if err != nil {
			return errors.GeneralError("error querying kafka instances for clusterID: %v", clusterID)
		}

		for _, instanceCount := range instanceCounts {
			if instanceCount.Clusterid == clusterID && instanceCount.Count > 0 {
				return errors.Forbidden("unable to deregister cluster with kafka instances")
			}
		}

		return nil
	}
}

func validateNoKafkaPromotionInProgress(kafkaRequest *dbapi.KafkaRequest) handlers.Validate {
	return func() *errors.ServiceError {
		if kafkaRequest.PromotionStatus == dbapi.KafkaPromotionStatusPromoting {
			return errors.GeneralError("promotion already in progress. kafka request %q is being promoted from kafka billing %q to %q", kafkaRequest.ID, kafkaRequest.ActualKafkaBillingModel, kafkaRequest.DesiredKafkaBillingModel)
		}

		return nil
	}
}

func validateRequestedKafkaPromotionHasDifferentKafkaBillingModel(kafkaPromoteRequest *public.KafkaPromoteRequest, kafkaRequest *dbapi.KafkaRequest) handlers.Validate {
	return func() *errors.ServiceError {
		if kafkaPromoteRequest.DesiredKafkaBillingModel == kafkaRequest.ActualKafkaBillingModel {
			return errors.BadRequest("kafka request %q already has %q kafka billing model", kafkaRequest.ID, kafkaRequest.ActualKafkaBillingModel)
		}

		return nil
	}
}

func validateKafkaRequestToPromoteHasAPromotableActualKafkaBillingModel(kafkaRequest *dbapi.KafkaRequest) handlers.Validate {
	return func() *errors.ServiceError {
		// TODO improve not referencing hardcoded strings
		if kafkaRequest.ActualKafkaBillingModel != "eval" {
			return errors.GeneralError("kafka request %q has a kafka billing model %q. Only kafka requests with a kafka billing model 'eval' can be promoted", kafkaRequest.ID, kafkaRequest.ActualKafkaBillingModel)
		}

		return nil
	}
}

func validateKafkaRequestToPromoteHasAPromotableStatus(kafkaRequest *dbapi.KafkaRequest) handlers.Validate {
	return func() *errors.ServiceError {
		acceptedKafkaStatusesForPromotion := []string{
			constants.KafkaRequestStatusReady.String(),
			constants.KafkaRequestStatusSuspended.String(),
			constants.KafkaRequestStatusResuming.String(),
		}

		found := arrays.Contains(acceptedKafkaStatusesForPromotion, kafkaRequest.Status)
		if !found {
			return errors.GeneralError("kafka request %q with status %q cannot be promoted: promotable status are: %+v", kafkaRequest.ID, kafkaRequest.Status, acceptedKafkaStatusesForPromotion)
		}

		return nil
	}
}

func validateKafkaPromoteRequestHasValidDesiredBillingModel(kafkaPromoteRequest *public.KafkaPromoteRequest) handlers.Validate {
	return func() *errors.ServiceError {

		// TODO improve not referencing hardcoded strings
		if kafkaPromoteRequest.DesiredKafkaBillingModel != "standard" && kafkaPromoteRequest.DesiredKafkaBillingModel != "marketplace" {
			return errors.FieldValidationError("desired kafka billing model %q promotion destination is not allowed", kafkaPromoteRequest.DesiredKafkaBillingModel)
		}
		return nil
	}
}

func validateKafkaPromoteRequestFields(kafkaPromoteRequest *public.KafkaPromoteRequest, kafkaRequest *dbapi.KafkaRequest, kafkaService services.KafkaService, kafkaConfig *config.KafkaConfig, kafkaPromoteValidatorFactory KafkaPromoteValidatorFactory) handlers.Validate {
	return func() *errors.ServiceError {
		desiredKafkaBillingModel := kafkaPromoteRequest.DesiredKafkaBillingModel

		svcErr := validateKafkaPromoteRequestHasValidDesiredBillingModel(kafkaPromoteRequest)()
		if svcErr != nil {
			return svcErr
		}

		// Run additional quota type specific promotion validators
		kafkaPromoteValidator, err := kafkaPromoteValidatorFactory.GetValidator(api.QuotaType(kafkaConfig.Quota.Type))
		if err != nil {
			return errors.New(errors.ToServiceError(err).Code, "error performing promotion: %s", err)
		}
		kafkaPromoteValidatorRequest := kafkaPromoteValidatorRequest{
			DesiredKafkaBillingModel:   desiredKafkaBillingModel,
			DesiredKafkaMarketplace:    kafkaPromoteRequest.DesiredMarketplace,
			DesiredKafkaCloudAccountID: kafkaPromoteRequest.DesiredBillingCloudAccountId,
			KafkaInstanceType:          kafkaRequest.InstanceType,
		}
		err = kafkaPromoteValidator.Validate(kafkaPromoteValidatorRequest)
		if err != nil {
			return errors.New(errors.ToServiceError(err).Code, "error performing promotion: %s", err)
		}

		// TODO improve not referencing hardcoded strings
		if kafkaPromoteRequest.DesiredKafkaBillingModel == "marketplace" {
			kafkaInstanceType := types.KafkaInstanceType(kafkaRequest.InstanceType)
			svcErr = kafkaService.ValidateBillingAccount(kafkaRequest.OrganisationId, kafkaInstanceType, kafkaPromoteRequest.DesiredKafkaBillingModel, kafkaPromoteRequest.DesiredBillingCloudAccountId, &kafkaPromoteRequest.DesiredMarketplace)
			if svcErr != nil {
				return svcErr
			}
		}

		return nil
	}
}
