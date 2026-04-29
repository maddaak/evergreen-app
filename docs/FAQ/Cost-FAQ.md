# Cost FAQ

Evergreen tracks the infrastructure cost of every task across three categories:

- **EC2** — the machine that runs the task. Cost is based on instance type and how long the task runs.
- **EBS** — the disk attached to the EC2 instance. GP3 volumes can be provisioned with higher throughput than the 125 MB/s free tier; the throughput cost above that baseline and the storage cost for the volume are both tracked.
- **S3** — object storage where task artifacts and logs are uploaded. Both the upload requests (PUT costs) and the ongoing storage are tracked.

All costs displayed in Evergreen have applicable discounts applied. Discount rates, the finance formula, and other cost parameters are maintained by the Financial Planning and Analysis (FP&A) team and configured in Evergreen's admin settings.

Cost information is shown on the task, version, and patch pages in the Evergreen UI.

- While a task is running, a predicted cost is shown; once it completes, the actual cost is shown.
- On version and patch pages, a running total of costs from completed tasks is shown throughout.

Once all tasks have finished, the full cost breakdown becomes available on each page, including a link to Honeycomb for a more detailed per-component view. Cost data is also available via the REST API — see [How can I view cost data via the REST API?](#how-can-i-view-cost-data-via-the-rest-api).

## What cost fields does Evergreen track?

Cost is tracked at the task level and rolls up to the version and patch level as tasks finish.

**Task-level fields:**

| Category | Subcategory      | Description                                                              |
| -------- | ---------------- | ------------------------------------------------------------------------ |
| EC2      |                  | Runtime cost with discounts applied                                      |
| EBS      | Throughput       | Cost above the 125 MB/s free tier, with discount applied                 |
| EBS      | Storage          | Cost for the attached volume, with discount applied                      |
| S3       | Artifact PUT     | PUT request cost for uploading user artifacts via the `s3.put` command   |
| S3       | Artifact storage | Storage cost for uploaded artifacts using S3 Intelligent Tiering pricing |
| S3       | Log PUT          | PUT request cost for uploading task logs                                 |
| S3       | Log storage      | Storage cost for uploaded logs over their retention period               |

All values reflect discounted costs. For field names and the full cost breakdown, see [How can I view cost data via the REST API?](#how-can-i-view-cost-data-via-the-rest-api).

**Version-level fields:**

The version aggregates costs across all its tasks as they finish. For patch-triggered versions, child patches run as separate versions with their own independent cost tracking — see [Child patch costs](#child-patch-costs).

## How is EC2 cost calculated?

EC2 is the machine that runs the task. The cost is calculated when the task finishes, using the task's actual runtime and the distro's pricing data.

The discounted cost blends a savings plan rate and the standard on-demand rate using a weighting factor:

```text
adjusted_cost = runtime_seconds * (finance_formula * savings_plan_rate + (1 - finance_formula) * on_demand_rate) / 3600
                                                                                                                   ^ EC2 rates are per-hour; divides to get per-second cost
```

`finance_formula` is a ratio between 0 and 1 that controls how much of the cost is attributed to MongoDB's savings plan coverage versus the standard AWS list price. The savings plan rate and `finance_formula` are provided by the FP&A team. The on-demand rate is the standard AWS list price for the instance type.

## How is EBS cost calculated?

EBS is the disk attached to the EC2 instance. Two components are tracked: throughput cost and storage cost. Both use us-east-1 rates regardless of where the task runs and are calculated when the task finishes.

### EBS throughput cost

GP3 volumes can be provisioned with higher throughput than the 125 MB/s AWS free tier. Only the throughput above that baseline is billable. If a distro has no GP3 mount points, or all volumes are at or below 125 MB/s, the throughput cost is zero.

```text
billable_throughput           = total_gp3_throughput_MBps - 125
adjusted_ebs_throughput_cost  = (billable_throughput * 0.04 / 2_592_000) * runtime_seconds * (1 - ebs_discount)
                                                               ^ seconds in a 30-day month
```

### EBS storage cost

Storage cost is based on the total size of all attached volumes, prorated to the task's actual runtime.

```text
adjusted_ebs_storage_cost = (volume_size_GB * 0.08 / 2_592_000) * runtime_seconds * (1 - ebs_discount)
                                                       ^ seconds in a 30-day month
```

The EBS discount rate applies to both throughput and storage.

## How is S3 cost calculated?

S3 is the object storage where task artifacts and logs are uploaded. Two types of cost are tracked: PUT request costs (charged per upload request) and storage costs (charged for how long the data is retained).

### Artifact PUT cost

Every S3 upload generates one or more PUT API requests depending on file size. Evergreen counts these requests and multiplies them by the AWS S3 PUT price, then applies an upload discount. PUT costs are calculated at the time of upload.

The number of PUT requests per file:

- Files under 5 MB: 1 PUT request.
- Files 5 MB and over: 1 (initiate) + number of 5 MB parts + 1 (complete). For example, a 12 MB file uses 5 PUT requests (1 initiate + 3 parts + 1 complete).

```text
s3_put_price                  = 0.000005  # $5 per million PUT requests (AWS standard rate)
adjusted_s3_artifact_put_cost = artifact_put_requests * s3_put_price * (1 - upload_cost_discount)
```

### Log PUT cost

Task logs are uploaded to S3 as the task runs; each upload uses a single PUT request. The cost uses the same rate and discount as artifact PUT costs and is calculated as logs are uploaded.

```text
s3_put_price             = 0.000005  # $5 per million PUT requests (AWS standard rate)
adjusted_s3_log_put_cost = log_put_requests * s3_put_price * (1 - upload_cost_discount)
```

### Storage cost

Artifact and log storage both use S3 Intelligent Tiering pricing, which automatically places objects into tiers based on their access patterns. Storage cost is calculated when the task finishes, using each bucket's S3 expiration lifecycle rule to determine the retention period. To simplify the cost calculations, Evergreen starts counting tier days from the day the objects are uploaded.

| Tier              | Days  | Price per GB-month |
| ----------------- | ----- | ------------------ |
| Standard          | 0–30  | $0.023             |
| Infrequent Access | 30–90 | $0.0125            |
| Archive           | 90+   | $0.004             |

The retention period is read from the bucket's S3 expiration lifecycle rule. If no expiration rule is found, Evergreen falls back to a system default retention period. Separate discounts can be configured for each tier. Costs are only calculated for DevProd-owned buckets.

:::note

Buckets without lifecycle rules are monitored — bucket owners are notified to add one.

:::

```text
size_gb          = upload_bytes / 1_073_741_824  # bytes to GB
days_in_standard = min(retention_days, 30)
days_in_ia       = max(0, min(retention_days, 90) - 30)
days_in_archive  = max(0, retention_days - 90)

adjusted_s3_storage_cost = size_gb * (
    (days_in_standard / 30) * 0.023  * (1 - standard_storage_discount) +
    (days_in_ia       / 30) * 0.0125 * (1 - ia_storage_discount)       +
    (days_in_archive  / 30) * 0.004  * (1 - archive_storage_discount)
)
```

## How is predicted cost calculated?

Predicted cost is an estimate of what the cost will be for a task, covering all cost categories (EC2, EBS, and S3). It is computed by averaging the historical costs for the same task — by name, project, and build variant — over the past week. If no matching task history exists within the past week, predicted cost is not available.

The `predicted_cost` field is returned on the task, version, and patch endpoints. On a version or patch, it represents the sum of predicted costs across all tasks. On patches, it also includes predicted costs for child patches.

Once tasks finish, the actual cost is calculated and stored separately. Predicted cost is not updated after tasks complete — it always reflects the pre-run estimate, not the final actual total.

## How can I view cost data via the REST API?

Cost fields are returned on the task, version, and patch endpoints. Discounted fields are prefixed `adjusted_` (for example, `adjusted_ec2_cost`). `on_demand_ec2_cost` is also returned as the undiscounted EC2 rate. EBS and S3 `on_demand_` equivalents are not returned by the API but are available in the Honeycomb cost breakdown.

**Task** — returns `task_cost` (discounted costs broken down by category) and `predicted_task_cost`. Also returns `s3_usage`, which contains the raw S3 upload metrics (PUT request counts and upload bytes) that Evergreen uses to calculate the S3 cost components in `task_cost`.

```text
GET https://evergreen.mongodb.com/rest/v2/tasks/{task_id}
```

**Version** — returns `cost` (aggregated discounted cost across all tasks in the version), `predicted_cost`, and `s3_usage` (aggregated artifact and log upload metrics across all tasks). `cost` accumulates as tasks finish and reflects the total cost of all completed tasks so far. Child patch costs are not included — see [Child patch costs](#child-patch-costs).

```text
GET https://evergreen.mongodb.com/rest/v2/versions/{version_id}
```

**Patch** — returns `cost` (aggregated discounted cost for the patch's own tasks), `predicted_cost` (including child patch predicted costs), and `s3_usage` (aggregated artifact and log upload metrics for the patch's own tasks). Child patch actual costs and S3 usage are not rolled up — see [Child patch costs](#child-patch-costs) for how to aggregate them. The [patch](../Reference/Glossary#patch-build) ID and version ID are the same value — both endpoints accept the same ID and return the same cost total.

```text
GET https://evergreen.mongodb.com/rest/v2/patches/{patch_id}
```

### Child patch costs

For patch-triggered builds that spawn child patches (for example, cross-repo PR patches), each child patch runs as a separate version. The REST API returns costs for each entity's own tasks only — child patch costs are **not** rolled up into the parent.

**What the Evergreen UI shows:** The UI aggregates child patch costs and displays a combined total across the parent and all children.

**What the REST API returns:** Each endpoint returns costs for that entity's own tasks only. To get the full total across a parent patch and all its children:

1. Query the parent patch endpoint — the response includes child patches under `child_patches`.
2. Extract each child's ID from `child_patches[].patch_id`.
3. Query each child patch endpoint and sum the `cost` fields yourself.

```text
GET /rest/v2/patches/{parent_patch_id}   → includes child_patches[].patch_id for each child
GET /rest/v2/patches/{child_patch_id_1}  → child cost
GET /rest/v2/patches/{child_patch_id_2}  → child cost
```
