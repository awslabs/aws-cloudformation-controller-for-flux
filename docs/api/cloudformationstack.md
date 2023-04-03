<h1>CloudFormationStack API reference</h1>
<p>Packages:</p>
<ul class="simple">
<li>
<a href="#cloudformation.contrib.fluxcd.io%2fv1alpha1">cloudformation.contrib.fluxcd.io/v1alpha1</a>
</li>
</ul>
<h2 id="cloudformation.contrib.fluxcd.io/v1alpha1">cloudformation.contrib.fluxcd.io/v1alpha1</h2>
<p>Package v2beta1 contains API Schema definitions for the CloudFormation v1alpha1 API group</p>
Resource Types:
<ul class="simple"></ul>
<h3 id="cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStack">CloudFormationStack
</h3>
<p>CloudFormationStack is the Schema for the CloudFormation stack API</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br>
<em>
<a href="#cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStackSpec">
CloudFormationStackSpec
</a>
</em>
</td>
<td>
<br/>
<br/>
<table>
<tr>
<td>
<code>stackName</code><br>
<em>
string
</em>
</td>
<td>
<p>Name of the CloudFormation stack.
Note that if this value is changed after creation, the controller will NOT
destroy the old stack and the old stack will no longer be updated by the controller.</p>
</td>
</tr>
<tr>
<td>
<code>templatePath</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Path to the CloudFormation template file.
Defaults to the root path of the SourceRef and filename &lsquo;template.yaml&rsquo;.</p>
</td>
</tr>
<tr>
<td>
<code>sourceRef</code><br>
<em>
<a href="#cloudformation.contrib.fluxcd.io/v1alpha1.SourceReference">
SourceReference
</a>
</em>
</td>
<td>
<p>SourceRef is the reference of the source where the CloudFormation template is stored.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<p>The interval at which to reconcile the CloudFormation stack.</p>
</td>
</tr>
<tr>
<td>
<code>pollInterval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>The interval at which to poll CloudFormation for the stack&rsquo;s status while a stack
action like Create or Update is in progress.
Defaults to five seconds.</p>
</td>
</tr>
<tr>
<td>
<code>retryInterval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>The interval at which to retry a previously failed reconciliation.
When not specified, the controller uses the CloudFormationStackSpec.Interval
value to retry failures.</p>
</td>
</tr>
<tr>
<td>
<code>suspend</code><br>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Suspend tells the controller to suspend reconciliation for this CloudFormation stack,
it does not apply to already started reconciliations. Defaults to false.</p>
</td>
</tr>
<tr>
<td>
<code>destroyStackOnDeletion</code><br>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Delete the CloudFormation stack and its underlying resources
upon deletion of this object. Defaults to false.</p>
</td>
</tr>
<tr>
<td>
<code>dependsOn</code><br>
<em>
<a href="https://godoc.org/github.com/fluxcd/pkg/apis/meta#NamespacedObjectReference">
[]github.com/fluxcd/pkg/apis/meta.NamespacedObjectReference
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>DependsOn may contain a meta.NamespacedObjectReference slice with
references to CloudFormationStack resources that must be ready before this CloudFormationStack
can be reconciled.</p>
</td>
</tr>
</table>
</td>
</tr>
<tr>
<td>
<code>status</code><br>
<em>
<a href="#cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStackStatus">
CloudFormationStackStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStackSpec">CloudFormationStackSpec
</h3>
<p>
(<em>Appears on:</em>
<a href="#cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStack">CloudFormationStack</a>)
</p>
<p>CloudFormationStackSpec defines the desired state of a CloudFormation stack</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>stackName</code><br>
<em>
string
</em>
</td>
<td>
<p>Name of the CloudFormation stack.
Note that if this value is changed after creation, the controller will NOT
destroy the old stack and the old stack will no longer be updated by the controller.</p>
</td>
</tr>
<tr>
<td>
<code>templatePath</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Path to the CloudFormation template file.
Defaults to the root path of the SourceRef and filename &lsquo;template.yaml&rsquo;.</p>
</td>
</tr>
<tr>
<td>
<code>sourceRef</code><br>
<em>
<a href="#cloudformation.contrib.fluxcd.io/v1alpha1.SourceReference">
SourceReference
</a>
</em>
</td>
<td>
<p>SourceRef is the reference of the source where the CloudFormation template is stored.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<p>The interval at which to reconcile the CloudFormation stack.</p>
</td>
</tr>
<tr>
<td>
<code>pollInterval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>The interval at which to poll CloudFormation for the stack&rsquo;s status while a stack
action like Create or Update is in progress.
Defaults to five seconds.</p>
</td>
</tr>
<tr>
<td>
<code>retryInterval</code><br>
<em>
<a href="https://godoc.org/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>The interval at which to retry a previously failed reconciliation.
When not specified, the controller uses the CloudFormationStackSpec.Interval
value to retry failures.</p>
</td>
</tr>
<tr>
<td>
<code>suspend</code><br>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Suspend tells the controller to suspend reconciliation for this CloudFormation stack,
it does not apply to already started reconciliations. Defaults to false.</p>
</td>
</tr>
<tr>
<td>
<code>destroyStackOnDeletion</code><br>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Delete the CloudFormation stack and its underlying resources
upon deletion of this object. Defaults to false.</p>
</td>
</tr>
<tr>
<td>
<code>dependsOn</code><br>
<em>
<a href="https://godoc.org/github.com/fluxcd/pkg/apis/meta#NamespacedObjectReference">
[]github.com/fluxcd/pkg/apis/meta.NamespacedObjectReference
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>DependsOn may contain a meta.NamespacedObjectReference slice with
references to CloudFormationStack resources that must be ready before this CloudFormationStack
can be reconciled.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStackStatus">CloudFormationStackStatus
</h3>
<p>
(<em>Appears on:</em>
<a href="#cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStack">CloudFormationStack</a>)
</p>
<p>CloudFormationStackStatus defines the observed state of a CloudFormation stack</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>observedGeneration</code><br>
<em>
int64
</em>
</td>
<td>
<em>(Optional)</em>
<p>ObservedGeneration is the last observed generation.</p>
</td>
</tr>
<tr>
<td>
<code>ReconcileRequestStatus</code><br>
<em>
<a href="https://godoc.org/github.com/fluxcd/pkg/apis/meta#ReconcileRequestStatus">
github.com/fluxcd/pkg/apis/meta.ReconcileRequestStatus
</a>
</em>
</td>
<td>
<p>
(Members of <code>ReconcileRequestStatus</code> are embedded into this type.)
</p>
</td>
</tr>
<tr>
<td>
<code>conditions</code><br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions holds the conditions for the CloudFormationStack.</p>
</td>
</tr>
<tr>
<td>
<code>lastAppliedRevision</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>LastAppliedRevision is the revision of the last successfully applied source.
The revision format for Git sources is <branch|tag>@sha1:<commit-sha>.</p>
</td>
</tr>
<tr>
<td>
<code>lastAttemptedRevision</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>LastAttemptedRevision is the revision of the last reconciliation attempt.
The revision format for Git sources is <branch|tag>@sha1:<commit-sha>.</p>
</td>
</tr>
<tr>
<td>
<code>lastAppliedChangeSet</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>LastAppliedChangeSet is the ARN of the last successfully applied CloudFormation change set.
The change set name format is flux-<generation>-<source-revision>.</p>
</td>
</tr>
<tr>
<td>
<code>lastAttemptedChangeSet</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>LastAttemptedChangeSet is the ARN of the CloudFormation change set for the last reconciliation attempt.
The change set name format is flux-<generation>-<source-revision>.</p>
</td>
</tr>
<tr>
<td>
<code>stackName</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>StackName is the name of the CloudFormation stack created by
the controller for the CloudFormationStack resource.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="cloudformation.contrib.fluxcd.io/v1alpha1.ReadinessUpdate">ReadinessUpdate
</h3>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>Reason</code><br>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>Message</code><br>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>SourceRevision</code><br>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>ChangeSetArn</code><br>
<em>
string
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<h3 id="cloudformation.contrib.fluxcd.io/v1alpha1.SourceReference">SourceReference
</h3>
<p>
(<em>Appears on:</em>
<a href="#cloudformation.contrib.fluxcd.io/v1alpha1.CloudFormationStackSpec">CloudFormationStackSpec</a>)
</p>
<p>Reference to a Flux source object.</p>
<div class="md-typeset__scrollwrap">
<div class="md-typeset__table">
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>apiVersion</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>API version of the source object.</p>
</td>
</tr>
<tr>
<td>
<code>kind</code><br>
<em>
string
</em>
</td>
<td>
<p>Kind of the source object.</p>
</td>
</tr>
<tr>
<td>
<code>name</code><br>
<em>
string
</em>
</td>
<td>
<p>Name of the source object.</p>
</td>
</tr>
<tr>
<td>
<code>namespace</code><br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Namespace of the source object, defaults to the namespace of the CloudFormation stack object.</p>
</td>
</tr>
</tbody>
</table>
</div>
</div>
<div class="admonition note">
<p class="last">This page was automatically generated with <code>gen-crd-api-reference-docs</code></p>
</div>
