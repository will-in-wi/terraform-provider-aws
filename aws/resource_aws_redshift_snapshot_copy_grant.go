package aws

import (
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/redshift"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceAwsRedshiftSnapshotCopyGrant() *schema.Resource {
	return &schema.Resource{
		// There is no API for updating/modifying grants, hence no Update
		// Instead changes to most fields will force a new resource
		Create: resourceAwsRedshiftSnapshotCopyGrantCreate,
		Read:   resourceAwsRedshiftSnapshotCopyGrantRead,
		Delete: resourceAwsRedshiftSnapshotCopyGrantDelete,
		Exists: resourceAwsRedshiftSnapshotCopyGrantExists,

		Schema: map[string]*schema.Schema{
			"snapshot_copy_grant_name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"kms_key_id": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},
		},
	}
}

func resourceAwsRedshiftSnapshotCopyGrantCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).redshiftconn

	grantName := d.Get("snapshot_copy_grant_name").(string)

	input := redshift.CreateSnapshotCopyGrantInput{
		SnapshotCopyGrantName: aws.String(grantName),
	}

	if v, ok := d.GetOk("kms_key_id"); ok {
		input.KmsKeyId = aws.String(v.(string))
	}

	log.Printf("[DEBUG]: Adding new Redshift SnapshotCopyGrant: %s", input)

	var out *redshift.CreateSnapshotCopyGrantOutput

	err := resource.Retry(3*time.Minute, func() *resource.RetryError {
		var err error

		out, err = conn.CreateSnapshotCopyGrant(&input)

		if err != nil {
			log.Printf("[ERROR] An error occured creating new AWS Redshift SnapshotCopyGrant: %s", err)
			return resource.NonRetryableError(err)
		}
		return nil
	})

	if err != nil {
		return err
	}

	log.Printf("[DEBUG] Created new Redshift SnapshotCopyGrant: %s", *out.SnapshotCopyGrant.SnapshotCopyGrantName)
	d.SetId(grantName)
	d.Set("snapshot_copy_grant_name", out.SnapshotCopyGrant.SnapshotCopyGrantName)
	d.Set("kms_key_id", out.SnapshotCopyGrant.KmsKeyId)

	return resourceAwsRedshiftSnapshotCopyGrantRead(d, meta)
}

func resourceAwsRedshiftSnapshotCopyGrantRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).redshiftconn

	grantName := d.Id()
	log.Printf("[DEBUG] Looking for grant: %s", grantName)
	grant, err := findAwsRedshiftSnapshotCopyGrantWithRetry(conn, grantName)

	if err != nil {
		return err
	}

	if grant == nil {
		log.Printf("[WARN] %s Redshift snapshot copy grant not found, removing from state file", grantName)
		d.SetId("")
		return nil
	}

	if *grant.KmsKeyId != "" {
		d.Set("kms_key_id", grant.KmsKeyId)
	}

	return nil
}

func resourceAwsRedshiftSnapshotCopyGrantDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).redshiftconn

	grantName := d.Id()

	deleteInput := redshift.DeleteSnapshotCopyGrantInput{
		SnapshotCopyGrantName: aws.String(grantName),
	}

	log.Printf("[DEBUG] Deleting snapshot copy grant: %s", grantName)
	_, err := conn.DeleteSnapshotCopyGrant(&deleteInput)

	if err != nil {
		if isAWSErr(err, redshift.ErrCodeSnapshotCopyGrantNotFoundFault, "") {
			return nil
		}
		return err
	}

	log.Printf("[DEBUG] Checking if grant is deleted: %s", grantName)
	err = waitForAwsRedshiftSnapshotCopyGrantToBeDeleted(conn, grantName)

	if err != nil {
		return err
	}

	return nil
}

func resourceAwsRedshiftSnapshotCopyGrantExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	conn := meta.(*AWSClient).redshiftconn

	grantName := d.Id()

	log.Printf("[DEBUG] Looking for Grant: %s", grantName)
	grant, err := findAwsRedshiftSnapshotCopyGrantWithRetry(conn, grantName)

	if err != nil {
		return true, err
	}
	if grant != nil {
		return true, err
	}

	return false, nil
}

func getAwsRedshiftSnapshotCopyGrant(grants []*redshift.SnapshotCopyGrant, grantName string) *redshift.SnapshotCopyGrant {
	for _, grant := range grants {
		if *grant.SnapshotCopyGrantName == grantName {
			return grant
		}
	}

	return nil
}

/*
In the functions below it is not possible to use retryOnAwsCodes function, as there
is no get grant call, so an error has to be created if the grant is or isn't returned
by the describe grants call when expected.
*/

// NB: This function only retries the grant not being returned and some edge cases, while AWS Errors
// are handled by the findAwsRedshiftSnapshotCopyGrant function
func findAwsRedshiftSnapshotCopyGrantWithRetry(conn *redshift.Redshift, grantName string) (*redshift.SnapshotCopyGrant, error) {
	var grant *redshift.SnapshotCopyGrant
	err := resource.Retry(3*time.Minute, func() *resource.RetryError {
		var err error
		grant, err = findAwsRedshiftSnapshotCopyGrant(conn, grantName, nil)

		if err != nil {
			if serr, ok := err.(AwsRedshiftSnapshotCopyGrantMissingError); ok {
				// Force a retry if the grant should exist
				return resource.RetryableError(serr)
			}

			return resource.NonRetryableError(err)
		}

		return nil
	})

	return grant, err
}

// Used by the tests as well
func waitForAwsRedshiftSnapshotCopyGrantToBeDeleted(conn *redshift.Redshift, grantName string) error {
	err := resource.Retry(3*time.Minute, func() *resource.RetryError {
		grant, err := findAwsRedshiftSnapshotCopyGrant(conn, grantName, nil)
		if err != nil {
			if isAWSErr(err, redshift.ErrCodeSnapshotCopyGrantNotFoundFault, "") {
				return nil
			}
		}

		if grant != nil {
			// Force a retry if the grant still exists
			return resource.RetryableError(
				fmt.Errorf("[DEBUG] Grant still exists while expected to be deleted: %s", *grant.SnapshotCopyGrantName))
		}

		return resource.NonRetryableError(err)
	})

	return err
}

// The DescribeSnapshotCopyGrants API defaults to listing only 100 grants
// Use a marker to iterate over all grants in "pages"
// NB: This function only retries on AWS Errors
func findAwsRedshiftSnapshotCopyGrant(conn *redshift.Redshift, grantName string, marker *string) (*redshift.SnapshotCopyGrant, error) {

	input := redshift.DescribeSnapshotCopyGrantsInput{
		MaxRecords: aws.Int64(int64(100)),
	}

	// marker and grant name are mutually exclusive
	if marker != nil {
		input.Marker = marker
	} else {
		input.SnapshotCopyGrantName = aws.String(grantName)
	}

	var out *redshift.DescribeSnapshotCopyGrantsOutput
	var err error
	var grant *redshift.SnapshotCopyGrant

	err = resource.Retry(3*time.Minute, func() *resource.RetryError {
		out, err = conn.DescribeSnapshotCopyGrants(&input)

		if err != nil {
			return resource.NonRetryableError(err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	grant = getAwsRedshiftSnapshotCopyGrant(out.SnapshotCopyGrants, grantName)
	if grant != nil {
		return grant, nil
	} else if out.Marker != nil {
		log.Printf("[DEBUG] Snapshot copy grant not found but marker returned, getting next page via marker: %s", aws.StringValue(out.Marker))
		return findAwsRedshiftSnapshotCopyGrant(conn, grantName, out.Marker)
	}

	return nil, NewAwsRedshiftSnapshotCopyGrantMissingError(fmt.Sprintf("[DEBUG] Grant %s not found", grantName))
}

// Custom error, so we don't have to rely on
// the content of an error message
type AwsRedshiftSnapshotCopyGrantMissingError string

func (e AwsRedshiftSnapshotCopyGrantMissingError) Error() string {
	return e.Error()
}

func NewAwsRedshiftSnapshotCopyGrantMissingError(msg string) AwsRedshiftSnapshotCopyGrantMissingError {
	return AwsRedshiftSnapshotCopyGrantMissingError(msg)
}
