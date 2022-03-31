/*
Copyright 2022 logan.

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

package controllers

import (
	"context"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appv1beta1 "github.com/shuboan-yun/opdemo/api/v1beta1"
)

var (
	oldSpecAnnotation = "old/spec"
)

// MyAppReconciler reconciles a MyApp object
type MyAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=app.qingcloud,resources=myapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=app.qingcloud,resources=myapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=app.qingcloud,resources=myapps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the MyApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *MyAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	var myapp appv1beta1.MyApp
	err := r.Client.Get(ctx, req.NamespacedName, &myapp)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// 在删除一个不存在的对象的时候, 可能会报not-found的错误
		// 这种情况不需要重新入队列排队修复
		return ctrl.Result{}, nil
	}
	// 当前对象标记为了删除
	if myapp.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	// 如果不存在关联资源, 是不是应该创新
	// 如果存在关联资源, 是不是要判断是否要更新
	deploy := &appsv1.Deployment{}
	if err := r.Client.Get(ctx, req.NamespacedName, deploy); err != nil && errors.IsNotFound(err) {
		// 1. 关联 Annotation
		data, err := json.Marshal(myapp.Spec)
		if err != nil {
			return ctrl.Result{}, err
		}

		if myapp.Annotations != nil {
			myapp.Annotations[oldSpecAnnotation] = string(data)
		} else {
			myapp.Annotations = map[string]string{
				oldSpecAnnotation: string(data),
			}
		}
		// 重新更新mysqpp
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.Client.Update(ctx, &myapp)
		}); err != nil {
			return ctrl.Result{}, err
		}
		// 1. 创建关联资源
		// 2. 创建Deployment
		newDeploy := NewDeploy(&myapp)
		if err := r.Client.Create(ctx, newDeploy); err != nil {
			return ctrl.Result{}, err
		}

		// 3. 创建Service
		newService := NewService(&myapp)
		if err := r.Client.Create(ctx, newService); err != nil {
			return ctrl.Result{}, err
		}
	}
	// r.Client.Get(ctxt, req.NamespacedName, deploy)
	oldSpec := appv1beta1.MyAppSpec{}
	if err := json.Unmarshal([]byte(myapp.Annotations[oldSpecAnnotation]), &oldSpec); err != nil {
		return ctrl.Result{}, err
	}
	// 是不是就可以来和新旧对象进行比较, 如果不一致就应该去更新
	// 更新关联资源
	if !reflect.DeepEqual(myapp.Spec, oldSpec) {
		NewDeploy := NewDeploy(&myapp)
		oldDeploy := &appsv1.Deployment{}
		if err := r.Client.Get(ctx, req.NamespacedName, oldDeploy); err != nil {
			return ctrl.Result{}, err
		}
		oldDeploy.Spec = NewDeploy.Spec
		// 正常就应该直接去更新oldDeploy
		// 注意, 一般情况不会直接调用Update进行更新
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.Client.Update(ctx, oldDeploy)
		}); err != nil {
			return ctrl.Result{}, err
		}
		// 更新Server
		newServer := NewService(&myapp)
		oldServer := &corev1.Service{}
		if err := r.Client.Get(ctx, req.NamespacedName, oldServer); err != nil {
			return ctrl.Result{}, err
		}
		oldServer.Spec = newServer.Spec
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := r.Client.Update(ctx, oldServer); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MyAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1beta1.MyApp{}).
		Complete(r)
}
