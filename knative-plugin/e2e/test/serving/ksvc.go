package serving

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/loft-sh/vcluster/e2e/framework"
	"github.com/loft-sh/vcluster/pkg/util/random"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	"github.com/onsi/ginkgo"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	ksvcv1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	KnativeServiceResourceKind = "Service"

	KnativeServiceName = "hello-ksvc"

	KnativeHelloV1Image = "gcr.io/google-samples/hello-app:1.0"
	KnativeHelloV2Image = "gcr.io/google-samples/hello-app:2.0"
)

var _ = ginkgo.Describe("Ksvc is synced down and applied as expected", func() {
	var (
		f *framework.Framework

		ns string

		vServingClient *servingclient.ServingV1Client
		pServingClient *servingclient.ServingV1Client
	)

	ginkgo.It("Initialize namespace and other base resources", func() {
		f = framework.DefaultFramework

		ns = fmt.Sprintf("e2e-knative-serving-%s", random.RandomString(5))

		var knativeClientErr error
		vServingClient, knativeClientErr = servingclient.NewForConfig(f.VclusterConfig)
		framework.ExpectNoError(knativeClientErr)

		pServingClient, knativeClientErr = servingclient.NewForConfig(f.HostConfig)
		framework.ExpectNoError(knativeClientErr)

		_, err := f.VclusterClient.CoreV1().Namespaces().Create(f.Context, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}, metav1.CreateOptions{})

		framework.ExpectNoError(err)
	})

	ginkgo.It("Test if ksvc CRD is synced", func() {
		resources, err := f.VclusterClient.DiscoveryClient.ServerResourcesForGroupVersion("serving.knative.dev/v1")
		framework.ExpectNoError(err, "Error encountered while fetching resources for serving.knative.dev/v1")

		var found bool
		for _, resource := range resources.APIResources {
			if resource.Kind == KnativeServiceResourceKind {
				found = true
			}
		}

		framework.ExpectNotEqual(found, false, "server does not recognise knative service, crd not synced")
	})

	ginkgo.It("Test create ksvc", func() {
		_, err := vServingClient.Services(ns).Create(f.Context, &ksvcv1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: KnativeServiceName,
			},
			Spec: ksvcv1.ServiceSpec{
				ConfigurationSpec: ksvcv1.ConfigurationSpec{
					Template: ksvcv1.RevisionTemplateSpec{
						Spec: ksvcv1.RevisionSpec{
							PodSpec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Image: KnativeHelloV1Image,
									},
								},
							},
						},
					},
				},
			},
		}, metav1.CreateOptions{})

		framework.ExpectNoError(err, "error creating ksvc in vcluster")
	})

	ginkgo.It("Test if ksvc synced down successfully", func() {
		err := wait.PollImmediate(time.Second, framework.PollTimeout, func() (bool, error) {
			_, err := pServingClient.Services(
				framework.DefaultVclusterNamespace).
				Get(f.Context,
					translate.PhysicalName(
						KnativeServiceName, ns),
					metav1.GetOptions{})
			if err != nil {
				if kerrors.IsNotFound(err) {
					return false, nil
				}

				return false, err
			}

			return true, nil
		})

		framework.ExpectNoError(err, fmt.Sprintf("unable to find physical service %s in namespace %s", translate.PhysicalName(KnativeServiceName, ns), framework.DefaultVclusterNamespace))
	})

	ginkgo.It("Test if virtual ksvc status synced up with physical ksvc", func() {
		pKsvc, err := pServingClient.Services(
			framework.DefaultVclusterNamespace).
			Get(f.Context,
				translate.PhysicalName(
					KnativeServiceName, ns),
				metav1.GetOptions{})
		framework.ExpectNoError(err)

		vKsvc, err := vServingClient.Services(ns).Get(f.Context, KnativeServiceName, metav1.GetOptions{})
		framework.ExpectNoError(err)

		framework.ExpectEqual(pKsvc.Status, vKsvc.Status, "expected virtual ksvc status to be in sync with physical ksvc")
	})

	ginkgo.It("Test if ksvc reachable at the published endpoint", func() {
		client := http.Client{
			Timeout: time.Second * 3,
		}

		vKsvc, err := vServingClient.Services(ns).Get(f.Context, KnativeServiceName, metav1.GetOptions{})
		framework.ExpectNoError(err)

		err = wait.PollImmediate(time.Second, framework.PollTimeout, func() (bool, error) {
			response, httpErr := client.Get(vKsvc.Status.URL.String())
			if httpErr != nil {
				klog.Errorf("error during GET request: %v", err)
				return false, nil
			}
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			if err != nil {
				klog.Errorf("error reading response body: %v", err)
				return false, nil
			}

			// check version

			if bodyMessage := bytes.Contains([]byte("Hello, world!"), body); !bodyMessage {
				klog.Errorf("body message does not match")
				return false, nil
			}

			if versionCheck := bytes.Contains([]byte("Version: 1.0.0"), body); !versionCheck {
				klog.Errorf("response version does not match")
				return false, nil
			}

			return true, nil
		})

		framework.ExpectNoError(err)
	})

	ginkgo.It("Test if ksvc traffic change to latest revision is synced down", func() {
		var vKsvc *ksvcv1.Service
		err := wait.PollImmediate(time.Millisecond*500, framework.PollTimeout, func() (bool, error) {
			var err error
			vKsvc, err = vServingClient.Services(ns).Get(f.Context, KnativeServiceName, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("unable to get vksvc: %v", err)
				return false, err
			}

			if len(vKsvc.Status.Traffic) == 0 {
				klog.Infof("vksvc traffic status not yet synced up from pksvc")
				return false, nil
			}

			return true, nil
		})

		framework.ExpectNoError(err, "error getting vksvc with a populated traffic status")

		// newLatestRevisionValue := false
		*vKsvc.Spec.Traffic[0].LatestRevision = false
		vKsvc.Spec.Traffic[0].RevisionName = vKsvc.Status.Traffic[0].RevisionName

		_, err = vServingClient.Services(ns).Update(f.Context, vKsvc, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		err = wait.PollImmediate(time.Millisecond*500, framework.PollTimeout, func() (bool, error) {
			pKsvc, err := pServingClient.Services(
				framework.DefaultVclusterNamespace).
				Get(f.Context,
					translate.PhysicalName(KnativeServiceName, ns),
					metav1.GetOptions{})
			if err != nil {
				if kerrors.IsNotFound(err) {
					return false, nil
				}

				return false, err
			}

			// make sure physical ksvc's traffic is explicitly set to initial revision
			// and not latest revision
			if *pKsvc.Spec.Traffic[0].LatestRevision {
				return false, nil
			}

			// make sure physical ksvc's traffic percent is fully directed to
			// initial version
			if *pKsvc.Spec.Traffic[0].Percent != 100 {
				return false, nil
			}

			return true, nil
		})

		framework.ExpectNoError(err)
	})

	ginkgo.It("Test if changing configuration image creates new revision", func() {
		vKsvc, err := vServingClient.Services(ns).Get(f.Context, KnativeServiceName, metav1.GetOptions{})
		framework.ExpectNoError(err)

		vKsvc.Spec.Template.Spec.Containers[0].Image = KnativeHelloV2Image

		_, err = vServingClient.Services(ns).Update(f.Context, vKsvc, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		err = wait.Poll(time.Millisecond*500, framework.PollTimeout, func() (bool, error) {
			revisions, err := pServingClient.Revisions(
				framework.DefaultFramework.VclusterNamespace).
				List(f.Context, metav1.ListOptions{})
			if err != nil {
				klog.Errorf("error getting physical revisions: %v", err)
				return false, nil
			}

			if len(revisions.Items) != 2 {
				klog.Errorf("number of revisions does not match: expected %d, got %d", 2, len(revisions.Items))
				return false, nil
			}

			return true, nil
		})

		framework.ExpectNoError(err)

		// check 2 separate revisions reflected in status
		err = wait.Poll(time.Millisecond*500, framework.PollTimeout, func() (bool, error) {
			vKsvc, err := vServingClient.Services(ns).Get(f.Context, KnativeServiceName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			currentRevision := vKsvc.Status.Traffic[0].RevisionName
			latestRevision := vKsvc.Status.LatestReadyRevisionName
			if currentRevision == latestRevision {
				klog.Info("new revision not yet ready")
				return false, nil
			}

			klog.Info("new revision created and ready")
			return true, nil
		})

		framework.ExpectNoError(err, "expected 2 separate revisions to be ready")
	})

	// this should always be the last spec
	ginkgo.It("Destroy namespace", func() {
		err := f.DeleteTestNamespace(ns, false)
		framework.ExpectNoError(err)
	})
})
