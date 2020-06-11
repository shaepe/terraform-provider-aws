package aws

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/guardduty"
	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
)

// Constants not currently provided by the AWS Go SDK
const (
	guardDutyPublishingStatusFailed = "FAILED"
)

func resourceAwsGuardDutyPublishingDestination() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsGuardDutyPublishingDestinationCreate,
		Read:   resourceAwsGuardDutyPublishingDestinationRead,
		Update: resourceAwsGuardDutyPublishingDestinationUpdate,
		Delete: resourceAwsGuardDutyPublishingDestinationDelete,

		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"detector_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"destination_type": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  guardduty.DestinationTypeS3,
			},
			"destination_arn": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validateArn,
			},
			"kms_key_arn": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validateArn,
			},
		},
	}
}

func resourceAwsGuardDutyPublishingDestinationCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).guarddutyconn

	detectorID := d.Get("detector_id").(string)
	input := guardduty.CreatePublishingDestinationInput{
		DetectorId: aws.String(detectorID),
		DestinationProperties: &guardduty.DestinationProperties{
			DestinationArn: aws.String(d.Get("destination_arn").(string)),
			KmsKeyArn:      aws.String(d.Get("kms_key_arn").(string)),
		},
		DestinationType: aws.String(d.Get("destination_type").(string)),
	}

	log.Printf("[DEBUG] Creating GuardDuty publishing destination: %s", input)
	output, err := conn.CreatePublishingDestination(&input)
	if err != nil {
		return fmt.Errorf("Creating GuardDuty publishing destination failed: %s", err.Error())
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{guardduty.PublishingStatusPendingVerification},
		Target:     []string{guardduty.PublishingStatusPublishing},
		Refresh:    guardDutyPublishingDestinationRefreshStatusFunc(conn, *output.DestinationId, detectorID),
		Timeout:    5 * time.Minute,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf("Error waiting for GuardDuty PublishingDestination status to be \"%s\": %s",
			guardduty.PublishingStatusPublishing, err)
	}

	d.SetId(fmt.Sprintf("%s:%s", d.Get("detector_id"), *output.DestinationId))

	return resourceAwsGuardDutyPublishingDestinationRead(d, meta)
}

func guardDutyPublishingDestinationRefreshStatusFunc(conn *guardduty.GuardDuty, destinationID, detectorID string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		input := &guardduty.DescribePublishingDestinationInput{
			DetectorId:    aws.String(detectorID),
			DestinationId: aws.String(destinationID),
		}
		resp, err := conn.DescribePublishingDestination(input)
		if err != nil {
			return nil, guardDutyPublishingStatusFailed, err
		}
		return resp, *resp.Status, nil
	}
}

func resourceAwsGuardDutyPublishingDestinationRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).guarddutyconn

	destinationId, detectorId, errStateRead := decodeGuardDutyPublishDestinationID(d.Id())

	if errStateRead != nil {
		return errStateRead
	}

	input := &guardduty.DescribePublishingDestinationInput{
		DetectorId:    aws.String(detectorId),
		DestinationId: aws.String(destinationId),
	}

	log.Printf("[DEBUG] Reading GuardDuty publishing destination: %s", input)
	gdo, err := conn.DescribePublishingDestination(input)
	if err != nil {
		if isAWSErr(err, guardduty.ErrCodeBadRequestException, "The request is rejected because the input detectorId is not owned by the current account.") {
			log.Printf("[WARN] GuardDuty publishing destination: %q not found, removing from state", d.Id())
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Reading GuardDuty publishing destination: '%s' failed: %s", d.Id(), err.Error())
	}

	d.Set("detector_id", detectorId)
	d.Set("destination_type", gdo.DestinationType)
	d.Set("kms_key_arn", gdo.DestinationProperties.KmsKeyArn)
	d.Set("destination_arn", gdo.DestinationProperties.DestinationArn)
	d.Set("status", gdo.Status)
	return nil
}

func resourceAwsGuardDutyPublishingDestinationUpdate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).guarddutyconn

	destinationId, detectorId, errStateRead := decodeGuardDutyPublishDestinationID(d.Id())

	if errStateRead != nil {
		return errStateRead
	}

	input := guardduty.UpdatePublishingDestinationInput{
		DestinationId: aws.String(destinationId),
		DetectorId:    aws.String(detectorId),
		DestinationProperties: &guardduty.DestinationProperties{
			DestinationArn: aws.String(d.Get("destination_arn").(string)),
			KmsKeyArn:      aws.String(d.Get("kms_key_arn").(string)),
		},
	}

	log.Printf("[DEBUG] Update GuardDuty publishing destination: %s", input)
	_, err := conn.UpdatePublishingDestination(&input)
	if err != nil {
		return fmt.Errorf("Updating GuardDuty publishing destination '%s' failed: %s", d.Id(), err.Error())
	}

	return resourceAwsGuardDutyPublishingDestinationRead(d, meta)
}

func resourceAwsGuardDutyPublishingDestinationDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).guarddutyconn

	destinationId, detectorId, errStateRead := decodeGuardDutyPublishDestinationID(d.Id())

	if errStateRead != nil {
		return errStateRead
	}

	input := guardduty.DeletePublishingDestinationInput{
		DestinationId: aws.String(destinationId),
		DetectorId:    aws.String(detectorId),
	}

	log.Printf("[DEBUG] Delete GuardDuty publishing destination: %s", input)
	_, err := conn.DeletePublishingDestination(&input)

	if isAWSErr(err, guardduty.ErrCodeBadRequestException, "") {
		return nil
	}

	if err != nil {
		return fmt.Errorf("Deleting GuardDuty publishing destination '%s' failed: %s", d.Id(), err.Error())
	}

	return nil
}

func decodeGuardDutyPublishDestinationID(id string) (destinationID, detectorID string, err error) {
	parts := strings.Split(id, ":")
	if len(parts) != 2 {
		err = fmt.Errorf("GuardDuty Publishing Destination ID must be of the form <Detector ID>:<Publishing Destination ID>, was provided: %s", id)
		return
	}
	destinationID = parts[1]
	detectorID = parts[0]
	return
}
