package controllers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	databasev1alpha1 "github.com/nowll/postgres-operator/api/v1alpha1"
)

type PostgresInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *PostgresInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	instance := &databasev1alpha1.PostgresInstance{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PostgresInstance resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get PostgresInstance")
		return ctrl.Result{}, err
	}

	if instance.Status.Phase == "" {
		instance.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	if instance.Spec.Replicas == 0 {
		instance.Spec.Replicas = 1
	}

	if err := r.reconcileSecret(ctx, instance); err != nil {
		return r.updateStatus(ctx, instance, "Failed", err.Error())
	}

	if err := r.reconcileConfigMap(ctx, instance); err != nil {
		return r.updateStatus(ctx, instance, "Failed", err.Error())
	}

	if err := r.reconcileStatefulSet(ctx, instance); err != nil {
		return r.updateStatus(ctx, instance, "Failed", err.Error())
	}

	if err := r.reconcileService(ctx, instance); err != nil {
		return r.updateStatus(ctx, instance, "Failed", err.Error())
	}

	if instance.Spec.Backup != nil && instance.Spec.Backup.Enabled {
		if err := r.reconcileBackupCronJob(ctx, instance); err != nil {
			return r.updateStatus(ctx, instance, "Failed", err.Error())
		}
	}

	ready, err := r.isStatefulSetReady(ctx, instance)
	if err != nil {
		return r.updateStatus(ctx, instance, "Failed", err.Error())
	}

	if ready {
		if err := r.reconcileDatabasesAndUsers(ctx, instance); err != nil {
			logger.Error(err, "Failed to reconcile databases and users, will retry")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		if err := r.updateEndpoints(ctx, instance); err != nil {
			logger.Error(err, "Failed to update endpoints")
		}

		return r.updateStatus(ctx, instance, "Running", "PostgreSQL instance is running")
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *PostgresInstanceReconciler) reconcileSecret(ctx context.Context, instance *databasev1alpha1.PostgresInstance) error {
	secret := &corev1.Secret{}
	secretName := fmt.Sprintf("%s-postgres-secret", instance.Name)

	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: instance.Namespace}, secret)
	if err != nil && errors.IsNotFound(err) {
		secret = r.buildSecret(instance, secretName)
		if err := ctrl.SetControllerReference(instance, secret, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, secret)
	}
	return err
}

func (r *PostgresInstanceReconciler) buildSecret(instance *databasev1alpha1.PostgresInstance, name string) *corev1.Secret {
	password := generatePassword(16)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    r.labelsForPostgres(instance),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"POSTGRES_PASSWORD": password,
			"POSTGRES_USER":     "postgres",
		},
	}
}

func (r *PostgresInstanceReconciler) reconcileConfigMap(ctx context.Context, instance *databasev1alpha1.PostgresInstance) error {
	configMap := &corev1.ConfigMap{}
	cmName := fmt.Sprintf("%s-postgres-config", instance.Name)

	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: instance.Namespace}, configMap)
	if err != nil && errors.IsNotFound(err) {
		configMap = r.buildConfigMap(instance, cmName)
		if err := ctrl.SetControllerReference(instance, configMap, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, configMap)
	}
	return err
}

func (r *PostgresInstanceReconciler) buildConfigMap(instance *databasev1alpha1.PostgresInstance, name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    r.labelsForPostgres(instance),
		},
		Data: map[string]string{
			"postgresql.conf": `max_connections = 100
shared_buffers = 256MB
effective_cache_size = 1GB
maintenance_work_mem = 64MB
checkpoint_completion_target = 0.9
wal_buffers = 16MB
default_statistics_target = 100
random_page_cost = 1.1
effective_io_concurrency = 200
work_mem = 2621kB
min_wal_size = 1GB
max_wal_size = 4GB
`,
		},
	}
}

func (r *PostgresInstanceReconciler) reconcileStatefulSet(ctx context.Context, instance *databasev1alpha1.PostgresInstance) error {
	sts := &appsv1.StatefulSet{}
	stsName := fmt.Sprintf("%s-postgres", instance.Name)

	err := r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: instance.Namespace}, sts)
	if err != nil && errors.IsNotFound(err) {
		sts = r.buildStatefulSet(instance, stsName)
		if err := ctrl.SetControllerReference(instance, sts, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, sts)
	} else if err != nil {
		return err
	}

	desiredReplicas := int32(instance.Spec.Replicas)
	if *sts.Spec.Replicas != desiredReplicas {
		sts.Spec.Replicas = &desiredReplicas
		return r.Update(ctx, sts)
	}

	return nil
}

func (r *PostgresInstanceReconciler) buildStatefulSet(instance *databasev1alpha1.PostgresInstance, name string) *appsv1.StatefulSet {
	labels := r.labelsForPostgres(instance)
	replicas := int32(instance.Spec.Replicas)

	storageSize := resource.MustParse(instance.Spec.Storage.Size)
	storageClass := instance.Spec.Storage.StorageClass

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: fmt.Sprintf("%s-postgres-headless", instance.Name),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: boolPtr(true),
						RunAsUser:    int64Ptr(999),
						FSGroup:      int64Ptr(999),
					},
					Containers: []corev1.Container{
						{
							Name:  "postgres",
							Image: fmt.Sprintf("postgres:%s", instance.Spec.Version),
							Ports: []corev1.ContainerPort{
								{
									Name:          "postgres",
									ContainerPort: 5432,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "POSTGRES_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: fmt.Sprintf("%s-postgres-secret", instance.Name),
											},
											Key: "POSTGRES_PASSWORD",
										},
									},
								},
								{
									Name:  "PGDATA",
									Value: "/var/lib/postgresql/data/pgdata",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "postgres-data",
									MountPath: "/var/lib/postgresql/data",
								},
								{
									Name:      "postgres-config",
									MountPath: "/etc/postgresql/postgresql.conf",
									SubPath:   "postgresql.conf",
								},
							},
							Resources: r.buildResourceRequirements(instance),
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"pg_isready", "-U", "postgres"},
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    6,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"pg_isready", "-U", "postgres"},
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "postgres-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: fmt.Sprintf("%s-postgres-config", instance.Name),
									},
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "postgres-data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						StorageClassName: &storageClass,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: storageSize,
							},
						},
					},
				},
			},
		},
	}
}

func (r *PostgresInstanceReconciler) buildResourceRequirements(instance *databasev1alpha1.PostgresInstance) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}

	if instance.Spec.Resources != nil {
		if instance.Spec.Resources.Requests.CPU != "" {
			resources.Requests[corev1.ResourceCPU] = resource.MustParse(instance.Spec.Resources.Requests.CPU)
		}
		if instance.Spec.Resources.Requests.Memory != "" {
			resources.Requests[corev1.ResourceMemory] = resource.MustParse(instance.Spec.Resources.Requests.Memory)
		}
		if instance.Spec.Resources.Limits.CPU != "" {
			resources.Limits[corev1.ResourceCPU] = resource.MustParse(instance.Spec.Resources.Limits.CPU)
		}
		if instance.Spec.Resources.Limits.Memory != "" {
			resources.Limits[corev1.ResourceMemory] = resource.MustParse(instance.Spec.Resources.Limits.Memory)
		}
	}

	return resources
}

func (r *PostgresInstanceReconciler) reconcileService(ctx context.Context, instance *databasev1alpha1.PostgresInstance) error {
	headlessSvc := &corev1.Service{}
	headlessName := fmt.Sprintf("%s-postgres-headless", instance.Name)

	err := r.Get(ctx, types.NamespacedName{Name: headlessName, Namespace: instance.Namespace}, headlessSvc)
	if err != nil && errors.IsNotFound(err) {
		headlessSvc = r.buildHeadlessService(instance, headlessName)
		if err := ctrl.SetControllerReference(instance, headlessSvc, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, headlessSvc); err != nil {
			return err
		}
	}

	svc := &corev1.Service{}
	svcName := fmt.Sprintf("%s-postgres", instance.Name)

	err = r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: instance.Namespace}, svc)
	if err != nil && errors.IsNotFound(err) {
		svc = r.buildService(instance, svcName)
		if err := ctrl.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, svc)
	}

	return err
}

func (r *PostgresInstanceReconciler) buildHeadlessService(instance *databasev1alpha1.PostgresInstance, name string) *corev1.Service {
	labels := r.labelsForPostgres(instance)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector:  labels,
			Ports: []corev1.ServicePort{
				{
					Name:     "postgres",
					Port:     5432,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
}

func (r *PostgresInstanceReconciler) buildService(instance *databasev1alpha1.PostgresInstance, name string) *corev1.Service {
	labels := r.labelsForPostgres(instance)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:     "postgres",
					Port:     5432,
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}
}

func (r *PostgresInstanceReconciler) reconcileBackupCronJob(ctx context.Context, instance *databasev1alpha1.PostgresInstance) error {
	cronjob := &batchv1.CronJob{}
	jobName := fmt.Sprintf("%s-backup", instance.Name)

	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: instance.Namespace}, cronjob)
	if err != nil && errors.IsNotFound(err) {
		cronjob = r.buildBackupCronJob(instance, jobName)
		if err := ctrl.SetControllerReference(instance, cronjob, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, cronjob)
	}
	return err
}

func (r *PostgresInstanceReconciler) buildBackupCronJob(instance *databasev1alpha1.PostgresInstance, name string) *batchv1.CronJob {
	labels := r.labelsForPostgres(instance)

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: instance.Spec.Backup.Schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:  "backup",
									Image: fmt.Sprintf("postgres:%s", instance.Spec.Version),
									Command: []string{
										"/bin/bash",
										"-c",
										fmt.Sprintf("PGPASSWORD=$POSTGRES_PASSWORD pg_dump -h %s-postgres.%s.svc.cluster.local -U postgres -Fc postgres | gzip > /tmp/backup-$(date +%%Y%%m%%d-%%H%%M%%S).sql.gz",
											instance.Name, instance.Namespace),
									},
									Env: []corev1.EnvVar{
										{
											Name: "POSTGRES_PASSWORD",
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: fmt.Sprintf("%s-postgres-secret", instance.Name),
													},
													Key: "POSTGRES_PASSWORD",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *PostgresInstanceReconciler) isStatefulSetReady(ctx context.Context, instance *databasev1alpha1.PostgresInstance) (bool, error) {
	sts := &appsv1.StatefulSet{}
	stsName := fmt.Sprintf("%s-postgres", instance.Name)

	if err := r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: instance.Namespace}, sts); err != nil {
		return false, err
	}

	return sts.Status.ReadyReplicas == *sts.Spec.Replicas, nil
}

func (r *PostgresInstanceReconciler) reconcileDatabasesAndUsers(ctx context.Context, instance *databasev1alpha1.PostgresInstance) error {
	secret := &corev1.Secret{}
	secretName := fmt.Sprintf("%s-postgres-secret", instance.Name)
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: instance.Namespace}, secret); err != nil {
		return err
	}

	password := string(secret.Data["POSTGRES_PASSWORD"])
	host := fmt.Sprintf("%s-postgres.%s.svc.cluster.local", instance.Name, instance.Namespace)

	connStr := fmt.Sprintf("host=%s port=5432 user=postgres password=%s dbname=postgres sslmode=disable", host, password)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	for _, user := range instance.Spec.Users {
		userPassword := generatePassword(16)
		_, err := db.Exec(fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", user.Name, userPassword))
		if err != nil && !isDuplicateError(err) {
			return fmt.Errorf("failed to create user %s: %w", user.Name, err)
		}
	}

	for _, database := range instance.Spec.Databases {
		_, err := db.Exec(fmt.Sprintf("CREATE DATABASE %s", database.Name))
		if err != nil && !isDuplicateError(err) {
			return fmt.Errorf("failed to create database %s: %w", database.Name, err)
		}

		if database.Owner != "" {
			_, err := db.Exec(fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", database.Name, database.Owner))
			if err != nil {
				return fmt.Errorf("failed to set owner for database %s: %w", database.Name, err)
			}
		}
	}

	for _, user := range instance.Spec.Users {
		for _, dbName := range user.Databases {
			_, err := db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", dbName, user.Name))
			if err != nil {
				return fmt.Errorf("failed to grant privileges to user %s on database %s: %w", user.Name, dbName, err)
			}
		}
	}

	return nil
}

func (r *PostgresInstanceReconciler) updateEndpoints(ctx context.Context, instance *databasev1alpha1.PostgresInstance) error {
	primary := fmt.Sprintf("%s-postgres-0.%s-postgres-headless.%s.svc.cluster.local:5432",
		instance.Name, instance.Name, instance.Namespace)

	var replicas []string
	for i := 1; i < instance.Spec.Replicas; i++ {
		replica := fmt.Sprintf("%s-postgres-%d.%s-postgres-headless.%s.svc.cluster.local:5432",
			instance.Name, i, instance.Name, instance.Namespace)
		replicas = append(replicas, replica)
	}

	instance.Status.Endpoints = databasev1alpha1.EndpointsSpec{
		Primary:  primary,
		Replicas: replicas,
	}

	return nil
}

func (r *PostgresInstanceReconciler) updateStatus(ctx context.Context, instance *databasev1alpha1.PostgresInstance, phase, message string) (ctrl.Result, error) {
	instance.Status.Phase = phase
	instance.Status.Message = message
	instance.Status.Ready = (phase == "Running")

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	if phase == "Failed" {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PostgresInstanceReconciler) labelsForPostgres(instance *databasev1alpha1.PostgresInstance) map[string]string {
	return map[string]string{
		"app":                                    "postgres",
		"postgres.database.example.com/instance": instance.Name,
	}
}

func generatePassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return base64.URLEncoding.EncodeToString(b)[:length]
}

func isDuplicateError(err error) bool {
	return err != nil && (err.Error() == "pq: role already exists" ||
		err.Error() == "pq: database already exists")
}

func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}

func (r *PostgresInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&databasev1alpha1.PostgresInstance{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}
