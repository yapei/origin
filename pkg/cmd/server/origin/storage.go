package origin

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/rest"
	authorizationclient "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset/typed/authorization/internalversion"

	projectapiv1 "github.com/openshift/origin/pkg/project/apis/project/v1"
	projectproxy "github.com/openshift/origin/pkg/project/registry/project/proxy"
	projectrequeststorage "github.com/openshift/origin/pkg/project/registry/projectrequest/delegated"
	routeapiv1 "github.com/openshift/origin/pkg/route/apis/route/v1"
	routeetcd "github.com/openshift/origin/pkg/route/registry/route/etcd"

	quotaapiv1 "github.com/openshift/origin/pkg/quota/apis/quota/v1"
	appliedclusterresourcequotaregistry "github.com/openshift/origin/pkg/quota/registry/appliedclusterresourcequota"
	clusterresourcequotaetcd "github.com/openshift/origin/pkg/quota/registry/clusterresourcequota/etcd"

	configapi "github.com/openshift/origin/pkg/cmd/server/api"
	securityapiv1 "github.com/openshift/origin/pkg/security/apis/security/v1"
	"github.com/openshift/origin/pkg/security/registry/podsecuritypolicyreview"
	"github.com/openshift/origin/pkg/security/registry/podsecuritypolicyselfsubjectreview"
	"github.com/openshift/origin/pkg/security/registry/podsecuritypolicysubjectreview"
	sccstorage "github.com/openshift/origin/pkg/security/registry/securitycontextconstraints/etcd"
	oscc "github.com/openshift/origin/pkg/security/securitycontextconstraints"

	// register api groups
	_ "github.com/openshift/origin/pkg/api/install"
)

// TODO this function needs to be broken apart with each API group owning their own storage, probably with two method
// per API group to give us legacy and current storage
func (c OpenshiftAPIConfig) GetRestStorage() (map[schema.GroupVersion]map[string]rest.Storage, error) {
	sccStorage := c.SCCStorage
	// TODO allow this when we're sure that its storing correctly and we want to allow starting up without embedding kube
	if false && sccStorage == nil {
		sccStorage = sccstorage.NewREST(c.GenericConfig.RESTOptionsGetter)
	}
	podSecurityPolicyReviewStorage := podsecuritypolicyreview.NewREST(
		oscc.NewDefaultSCCMatcher(c.SecurityInformers.Security().InternalVersion().SecurityContextConstraints().Lister()),
		c.KubeInternalInformers.Core().InternalVersion().ServiceAccounts().Lister(),
		c.KubeClientInternal,
	)
	podSecurityPolicySubjectStorage := podsecuritypolicysubjectreview.NewREST(
		oscc.NewDefaultSCCMatcher(c.SecurityInformers.Security().InternalVersion().SecurityContextConstraints().Lister()),
		c.KubeClientInternal,
	)
	podSecurityPolicySelfSubjectReviewStorage := podsecuritypolicyselfsubjectreview.NewREST(
		oscc.NewDefaultSCCMatcher(c.SecurityInformers.Security().InternalVersion().SecurityContextConstraints().Lister()),
		c.KubeClientInternal,
	)

	authorizationClient, err := authorizationclient.NewForConfig(c.GenericConfig.LoopbackClientConfig)
	if err != nil {
		return nil, err
	}
	routeStorage, routeStatusStorage, err := routeetcd.NewREST(c.GenericConfig.RESTOptionsGetter, c.RouteAllocator, authorizationClient.SubjectAccessReviews())
	if err != nil {
		return nil, fmt.Errorf("error building REST storage: %v", err)
	}

	projectStorage := projectproxy.NewREST(c.KubeClientInternal.Core().Namespaces(), c.ProjectAuthorizationCache, c.ProjectAuthorizationCache, c.ProjectCache)

	namespace, templateName, err := configapi.ParseNamespaceAndName(c.ProjectRequestTemplate)
	if err != nil {
		glog.Errorf("Error parsing project request template value: %v", err)
		// we can continue on, the storage that gets created will be valid, it simply won't work properly.  There's no reason to kill the master
	}

	projectRequestStorage := projectrequeststorage.NewREST(
		c.ProjectRequestMessage,
		namespace, templateName,
		c.DeprecatedOpenshiftClient,
		c.GenericConfig.LoopbackClientConfig,
		c.KubeInternalInformers.Rbac().InternalVersion().RoleBindings().Lister(),
	)

	clusterResourceQuotaStorage, clusterResourceQuotaStatusStorage, err := clusterresourcequotaetcd.NewREST(c.GenericConfig.RESTOptionsGetter)
	if err != nil {
		return nil, fmt.Errorf("error building REST storage: %v", err)
	}

	storage := map[schema.GroupVersion]map[string]rest.Storage{}

	storage[quotaapiv1.SchemeGroupVersion] = map[string]rest.Storage{
		"clusterResourceQuotas":        clusterResourceQuotaStorage,
		"clusterResourceQuotas/status": clusterResourceQuotaStatusStorage,
		"appliedClusterResourceQuotas": appliedclusterresourcequotaregistry.NewREST(
			c.ClusterQuotaMappingController.GetClusterQuotaMapper(),
			c.QuotaInformers.Quota().InternalVersion().ClusterResourceQuotas().Lister(),
			c.KubeInternalInformers.Core().InternalVersion().Namespaces().Lister(),
		),
	}

	storage[securityapiv1.SchemeGroupVersion] = map[string]rest.Storage{
		"securityContextConstraints":          sccStorage,
		"podSecurityPolicyReviews":            podSecurityPolicyReviewStorage,
		"podSecurityPolicySubjectReviews":     podSecurityPolicySubjectStorage,
		"podSecurityPolicySelfSubjectReviews": podSecurityPolicySelfSubjectReviewStorage,
	}

	storage[projectapiv1.SchemeGroupVersion] = map[string]rest.Storage{
		"projects":        projectStorage,
		"projectRequests": projectRequestStorage,
	}

	storage[routeapiv1.SchemeGroupVersion] = map[string]rest.Storage{
		"routes":        routeStorage,
		"routes/status": routeStatusStorage,
	}

	return storage, nil
}
