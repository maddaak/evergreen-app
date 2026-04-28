# Cost FAQ

Evergreen tracks the infrastructure cost of every task across three categories:

- **EC2** — the virtual machine that runs the task. Cost is based on instance type and how long the task runs.
- **EBS** — the disk attached to the EC2 instance. GP3 volumes can be provisioned with higher throughput than the 125 MB/s free tier; the throughput cost above that baseline and the storage cost for the volume are both tracked.
- **S3** — object storage where task artifacts and logs are uploaded. Both the upload requests (PUT costs) and the ongoing storage are tracked.

All costs displayed in Evergreen have applicable discounts applied. Discount rates are configured by the [BizOps team](https://wiki.corp.mongodb.com/spaces/ADS/pages/143691615/Business+Operations+Team+BizOps) and set in Evergreen's admin settings. Non-discounted list-price values are also available for each cost field via the REST API — see [How can I view cost data via the REST API?](#how-can-i-view-cost-data-via-the-rest-api).

## What cost fields does Evergreen track?

Cost is tracked at the task level and rolls up to the version level once all tasks finish.

**Task-level fields:**

| Category | Description                                                         |
| -------- | ------------------------------------------------------------------- |
| EC2      | Runtime cost with discounts applied                                 |
| EBS      | Throughput cost above the 125 MB/s free tier, with discount applied |
| EBS      | Storage cost for the attached volume, with discount applied         |
| S3       | PUT request cost for uploading user artifacts                       |
| S3       | Storage cost for uploaded artifacts over their retention period     |
| S3       | PUT request cost for uploading task log chunks                      |
| S3       | Storage cost for uploaded logs over their retention period          |

All values reflect discounted costs. For field names, non-discounted list-price equivalents, and the full cost breakdown, see [How can I view cost data via the REST API?](#how-can-i-view-cost-data-via-the-rest-api).

**Version-level fields:**

The version aggregates costs across all its tasks once they finish. A predicted cost estimate is also available on both the task and version while tasks are still running — see [How is predicted cost calculated?](#how-is-predicted-cost-calculated).

## How is EC2 cost calculated?

EC2 is the virtual machine that runs the task. The cost is calculated when the task finishes, using the task's actual runtime and the distro's pricing data.

The discounted cost blends MongoDB's AWS savings plan rate and the standard on-demand rate using a weighting factor:

```text
adjusted_cost = runtime_seconds * (finance_formula * savings_plan_rate + (1 - finance_formula) * on_demand_rate) / 3600
                                                                                                                   ^ EC2 rates are per-hour; divides to get per-second cost
```

`finance_formula` is a ratio between 0 and 1 that controls how much of the cost is attributed to MongoDB's savings plan coverage versus the standard AWS list price. The savings plan rate and `finance_formula` are provided by the [BizOps team](https://wiki.corp.mongodb.com/spaces/ADS/pages/143691615/Business+Operations+Team+BizOps). The on-demand rate is the standard AWS list price for the instance type.

## How is EBS cost calculated?

EBS is the disk attached to the EC2 instance. Two components are tracked: throughput cost and storage cost. Both use us-east-1 rates regardless of where the task runs and are calculated when the task finishes.

### EBS throughput cost

GP3 volumes can be provisioned with higher throughput than the 125 MB/s AWS free tier. Only the throughput above that baseline is billable. If a distro has no GP3 mount points, or all volumes are at or below 125 MB/s, the throughput cost is zero.

```text
billable_throughput = total_gp3_throughput_MBps - 125
adjusted_ebs_throughput_cost = (billable_throughput * 0.04 / 2_592_000) * runtime_seconds * (1 - ebs_discount)
                                                          ^ seconds in a 30-day month
```

### EBS storage cost

Storage cost is based on the total size of all attached volumes, prorated to the task's actual runtime. If a distro has no mount points, the storage cost is zero.

```text
adjusted_ebs_storage_cost = (volume_size_GB * 0.08 / 2_592_000) * runtime_seconds * (1 - ebs_discount)
                                                    ^ seconds in a 30-day month
```

The EBS discount rate applies to both throughput and storage and is provided by the [BizOps team](https://wiki.corp.mongodb.com/spaces/ADS/pages/143691615/Business+Operations+Team+BizOps).

## How is S3 cost calculated?

S3 is the object storage where task artifacts and logs are uploaded. Two types of cost are tracked: PUT request costs (charged per upload request) and storage costs (charged for how long the data is retained).

### Artifact PUT cost

Every S3 upload generates one or more PUT API requests depending on file size. Evergreen counts these requests and multiplies by the AWS S3 PUT price ($0.000005 per request), then applies an upload discount. PUT costs are calculated at the time of upload.

The number of PUT requests per file:

- Files under 5 MB: 1 PUT request.
- Files 5 MB and over: 1 (initiate) + number of 5 MB parts + 1 (complete). For example, a 12 MB file uses 5 PUT requests (1 initiate + 3 parts + 1 complete).

```text
adjusted_s3_artifact_put_cost = artifact_put_requests * 0.000005 * (1 - upload_cost_discount)
```

The upload discount is provided by the [BizOps team](https://wiki.corp.mongodb.com/spaces/ADS/pages/143691615/Business+Operations+Team+BizOps).

### Artifact storage cost

Storage cost uses S3 Intelligent Tiering pricing, which transitions objects through three tiers based on their retention period. Storage cost is calculated when the task finishes, using the bucket's lifecycle rule to determine how long the artifacts will be retained.

| Tier              | Days  | Price per GB-month |
| ----------------- | ----- | ------------------ |
| Standard          | 0–30  | $0.023             |
| Infrequent Access | 30–90 | $0.0125            |
| Archive           | 90+   | $0.004             |

The retention period is read from the bucket's S3 lifecycle rule (when possible). An S3 lifecycle rule is an AWS policy that defines how long objects are retained before being deleted or transitioned to a cheaper storage tier. If no lifecycle rule is found, the `default_max_artifact_expiration_days` admin setting is used as a fallback — this is a global default configured by the Evergreen admin team. If neither is available, storage cost is not calculated. Discount rates for each storage tier are provided by the [BizOps team](https://wiki.corp.mongodb.com/spaces/ADS/pages/143691615/Business+Operations+Team+BizOps).

```text
days_in_standard = min(expiration_days, 30)
days_in_ia       = max(0, min(expiration_days, 90) - 30)
days_in_archive  = max(0, expiration_days - 90)

adjusted_s3_artifact_storage_cost = upload_bytes / GB / 30
    * (days_in_standard * 0.023  * (1 - standard_storage_cost_discount))
    + (days_in_ia       * 0.0125 * (1 - i_a_storage_cost_discount))
    + (days_in_archive  * 0.004  * (1 - archive_storage_cost_discount))
```

`upload_bytes / GB` converts bytes to gigabytes; `/ 30` converts the per-month tier price to a per-day rate.

### Log PUT cost

Task logs are uploaded to S3 in chunks as the task runs; each chunk uses a single PUT request. The cost uses the same rate and discount as artifact PUT costs and is calculated as logs are uploaded.

```text
adjusted_s3_log_put_cost = log_put_requests * 0.000005 * (1 - upload_cost_discount)
```

### Log storage cost

Task logs are stored in a separate S3 bucket from artifacts. Log storage uses the same Intelligent Tiering formula as artifact storage, applied to the uploaded log bytes and the log bucket's lifecycle rule. Log storage cost is calculated when the task finishes.

### When are S3 costs calculated?

Artifact PUT costs are only calculated if the task uploaded files via `s3.put`. Log PUT costs are incurred whenever a task produces logs. S3 storage costs require either a resolvable lifecycle rule for the bucket or a configured `default_max_artifact_expiration_days` admin setting. If neither is available, storage cost is not calculated.

## How is predicted cost calculated?

Predicted cost is an estimate of the total task cost before a task finishes, covering all cost categories (EC2, EBS, and S3). It is computed by averaging the actual costs for the same task — by name, project, and build variant — over the past week. If no matching task history exists within the past week, predicted cost is not available.

Predicted cost is available on both the task and version endpoints while tasks are running. On a version, it represents the sum of predicted costs across all of its tasks.

Once tasks finish, the actual cost is calculated and stored separately. Predicted cost is not updated after tasks complete — it always reflects the pre-run estimate, not the final actual total.

## How can I view cost data via the REST API?

Cost fields are returned on the task and version endpoints. For full authentication and usage details, see the [REST API documentation](../API/REST-V2-Usage).

Discounted fields are prefixed `adjusted_` and non-discounted list-price fields are prefixed `on_demand_` (for example, `adjusted_ec2_cost` and `on_demand_ec2_cost`).

**Task** — returns `task_cost` (discounted costs broken down by category) and `predicted_task_cost`. Also returns `s3_usage`, which contains the raw S3 upload metrics (PUT request counts and upload bytes) that Evergreen uses as inputs to calculate the S3 cost components in `task_cost`.

```text
GET https://evergreen.mongodb.com/rest/v2/tasks/{task_id}
```

**Version** — returns `cost` (aggregated discounted cost across all tasks), `predicted_cost`, and `s3_usage` (aggregated artifact and log upload metrics across all tasks). `cost` is only populated once all tasks have finished running.

```text
GET https://evergreen.mongodb.com/rest/v2/versions/{version_id}
```

Both endpoints require authentication. For full details, see the [REST API documentation](../API/REST-V2-Usage).
