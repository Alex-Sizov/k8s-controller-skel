package main
import (
        "fmt"
        "flag"
        "os"
        "context"
        "sigs.k8s.io/controller-runtime/pkg/client/config"
        "sigs.k8s.io/controller-runtime/pkg/manager"
        "k8s.io/apimachinery/pkg/api/errors"
        myv1alpha1 "k8s-controller-skel/pkg/apis/mygroup.example.com/v1alpha1"
        corev1 "k8s.io/api/core/v1"
        appsv1 "k8s.io/api/apps/v1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime"
        clientgoscheme "k8s.io/client-go/kubernetes/scheme"
        "sigs.k8s.io/controller-runtime/pkg/controller"
        "sigs.k8s.io/controller-runtime/pkg/reconcile"
        "sigs.k8s.io/controller-runtime/pkg/source"
        "sigs.k8s.io/controller-runtime/pkg/handler"
        "sigs.k8s.io/controller-runtime/pkg/client"
        "sigs.k8s.io/controller-runtime/pkg/cache"
        "sigs.k8s.io/controller-runtime/pkg/log"
        "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type MyReconciler struct {
        client client.Client
        cache cache.Cache
        scheme *runtime.Scheme
}

const(
        _buildingState = "Building"
        _readyState     = "Ready"
        operatorName = "my-operator"
)

func main() {
        flag.Parse()
        log.SetLogger(zap.New())
	scheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(scheme)
	myv1alpha1.AddToScheme(scheme)
	mgr, err := manager.New(
		config.GetConfigOrDie(),
		manager.Options{
			Scheme: scheme,
		},
	)
	fatalErr(err, "Can't init manager")
	ctrl, err := controller.New(
		operatorName, mgr,
		controller.Options{
			Reconciler: &MyReconciler{
				client: mgr.GetClient(),
				cache: mgr.GetCache(),
				scheme: mgr.GetScheme(),
			},
		})
	fatalErr(err, "Can't init controller")
	err = ctrl.Watch(
		source.Kind(
			mgr.GetCache(),
			&myv1alpha1.MyResource{},
		),
		&handler.EnqueueRequestForObject{},
	)
	fatalErr(err, "Can't init watcher")
	err = ctrl.Watch(
		source.Kind(
			mgr.GetCache(),
			&appsv1.Deployment{},
		),
		handler.EnqueueRequestForOwner(
			mgr.GetScheme(),
			mgr.GetRESTMapper(),
			&myv1alpha1.MyResource{},
			handler.OnlyControllerOwner()),
	)
	fatalErr(err, "Can't init watcher")
	err = mgr.Start(context.Background())
	fatalErr(err, "Can't start manager")
}

func fatalErr(err error, desc string) {
        if err != nil {
                log.Log.Error(err, desc)
                os.Exit(1)
        }
}

func (r *MyReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
        log := log.FromContext(ctx)
        log.Info("getting myresource instance")
	myresource := myv1alpha1.MyResource{}
	err := r.client.Get(
		ctx,
		req.NamespacedName,
		&myresource,
		&client.GetOptions{},
	)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("resource is not found")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	deploy := createDeployment(&myresource)
	err = r.client.Patch(
		ctx,
		deploy,
		client.Apply,
		client.FieldOwner(operatorName),
		client.ForceOwnership,
	)
	if err != nil {
		return reconcile.Result{}, err
	}
	status, err := r.computeStatus(ctx, &myresource)
	if err != nil {
		return reconcile.Result{}, err
	}
	myresource.Status = status
	log.Info("updating status", "state", status.State)
	err = r.client.Status().Update(ctx, &myresource)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func createDeployment(myres *myv1alpha1.MyResource) *appsv1.Deployment {
        deploy := &appsv1.Deployment{
                ObjectMeta: metav1.ObjectMeta{
                        Name: fmt.Sprintf("%s-deployment", myres.GetName()),
                        Namespace: myres.GetNamespace(),
                        Labels: map[string]string{
                                "myresource": myres.GetName(),
                        },
                },
                Spec: appsv1.DeploymentSpec{
                        Selector: &metav1.LabelSelector{
                                MatchLabels: map[string]string{
                                        "myresource": myres.GetName(),
                                },
                        },
                        Template: corev1.PodTemplateSpec{
                                ObjectMeta: metav1.ObjectMeta{
                                        Labels: map[string]string{
                                                "myresource": myres.GetName(),
                                        },
                                },
                                Spec: corev1.PodSpec{
                                        Containers: []corev1.Container{
                                                {
                                                        Name:  "main",
                                                        Image: myres.Spec.Image,
                                                        Resources: corev1.ResourceRequirements{
                                                                Requests: corev1.ResourceList{
                                                                        corev1.ResourceMemory: myres.Spec.Memory,
                                                                },
                                                        },
                                                },
                                        },
                                },
                        },
                },
        }
        deploy.SetGroupVersionKind(
                appsv1.SchemeGroupVersion.WithKind("Deployment"),
        )
        ownerReference := metav1.NewControllerRef(
                myres,
                myv1alpha1.SchemeGroupVersion.
                        WithKind("MyResource"),
        )
        deploy.SetOwnerReferences([]metav1.OwnerReference{
                *ownerReference,
        })
        return deploy
}

func (r *MyReconciler) computeStatus(ctx context.Context, myres *myv1alpha1.MyResource) (myv1alpha1.MyResourceStatus, error) {
        logger := log.FromContext(ctx)
        result := myv1alpha1.MyResourceStatus{
                State: _buildingState,
        }
        deployList := appsv1.DeploymentList{}
        err := r.client.List(
                ctx,
                &deployList,
                client.InNamespace(myres.GetNamespace()),
                client.MatchingLabels{
                        "myresource": myres.GetName(),
                },
        )
        if err != nil {
                return result, err
        }
        if len(deployList.Items) == 0 {
                logger.Info("no deployment found")
                return result, nil
        }
        if len(deployList.Items) > 1 {
                return result, fmt.Errorf(
                        "%d deployment found, expected 1",
                        len(deployList.Items),
                )
        }
        status := deployList.Items[0].Status
        logger.Info(
                "got deployment status",
                "status", status,
        )
        if status.ReadyReplicas == 1 {
                result.State = _readyState
        }
        return result, nil
}
