package aws

import (
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/logrusorgru/aurora"
)

// colorizer
var style aurora.Aurora

func checkCredentialsEnvVar() bool {

	if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {

		return false

	} else if os.Getenv("AWS_REGION") == "" {

		if os.Getenv("AWS_DEFAULT_REGION") == "" {
			return false
		}
		os.Setenv("AWS_REGION", os.Getenv("AWS_DEFAULT_REGION"))

	}
	return true
}

// ReadFile reads a file from S3 bucket and saves it in a desired location.
func ReadFile(bucketName string, filename string, outFile string, noColors bool) {
	style = aurora.NewAurora(!noColors)
	// Checking env vars are set to configure AWS
	if !checkCredentialsEnvVar() {
		log.Println("WARN: Failed to find the AWS env vars needed to configure AWS. Please make sure they are set in the environment.")
	}

	// Create Session -- use config (credentials + region) from env vars or aws profile
	sess, err := session.NewSession()

	if err != nil {
		log.Fatal(style.Bold(style.Red("ERROR: Can't create AWS session: " + err.Error())))
	}
	// create S3 download manger
	downloader := s3manager.NewDownloader(sess)

	file, err := os.Create(outFile)
	if err != nil {
		log.Fatal(style.Bold(style.Red("ERROR: Failed to open file " + outFile + ": " + err.Error())))
	}

	defer file.Close()

	_, err = downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(filename),
		})
	if err != nil {
		log.Fatal(style.Bold(style.Red("ERROR: Failed to download file  " + filename + " from S3: " + err.Error())))
	}

	log.Println("Successfully downloaded " + filename + " from S3 as " + outFile)

}


// ReadSSMParam reads a value from an SSM Parameter
func ReadSSMParam(keyname string, withDecryption bool, noColors bool) string  {
	style = aurora.NewAurora(!noColors)

	// Checking env vars are set to configure AWS
	if !checkCredentialsEnvVar() {
		log.Println("WARN: Failed to find the AWS env vars needed to configure AWS. Please make sure they are set in the environment.")
	}

	// Create Session -- use config (credentials + region) from env vars or aws profile
	sess, err := session.NewSession()

	if err != nil {
		log.Fatal(style.Bold(style.Red("ERROR: Can't create AWS session: " + err.Error())))
	}

	ssmsvc := ssm.New(sess, aws.NewConfig())
	param, err := ssmsvc.GetParameter(&ssm.GetParameterInput{
		Name:           &keyname,
		WithDecryption: &withDecryption,
	})

	if err != nil {
		log.Fatal(style.Bold(style.Red("ERROR: Can't find the SSM Parameter " + keyname + " : " + err.Error())))
	}

	value := *param.Parameter.Value
	return value
}