package main

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

var (
	s3Client *s3.S3
	wg       sync.WaitGroup
)

var s3Bucket = "BUCKET_NAME"

func init() {
	sess, err := session.NewSession(
		&aws.Config{
			Region: aws.String("sa-east-1"), // Verificar se é o mesmo que está setado na AWS
			Credentials: credentials.NewStaticCredentials(
				"ACCESS_KEY_ID",     // Access key ID
				"SECRET_ACCESS_KEY", // Secret access key
				"",                  // Token, se tiver
			),
		},
	)

	if err != nil {
		panic(err)
	}

	s3Client = s3.New(sess)
}

func main() {
	dir, err := os.Open("./tmp")
	if err != nil {
		panic(err)
	}

	defer dir.Close()
	uploadControl := make(chan struct{}, 100) // limita a quantidade em 100, neste exemplo
	errorFileUpload := make(chan string, 10)  // Crio um canal com os nomes dos arquivos que deram erro

	// Função que fica verificando se o canal de erros possui alguma coisa. Se sim, executa a função novamente
	go func() {
		for {
			select {
			case filename := <-errorFileUpload:
				uploadControl <- struct{}{}
				wg.Add(1)
				go uploadFile(filename, uploadControl, errorFileUpload)
			}
		}
	}()

	for {
		files, err := dir.ReadDir(1)
		if err != nil {
			if err == io.EOF { // end of file
				break
			}
			fmt.Printf("Error reading directory: %s\n", err)
			continue
		}
		wg.Add(1)
		uploadControl <- struct{}{} // Vai adicionando uma struct vazia até encher o channel. Quando encher, não vai
		// permitir que execute a go routine até que seja menor que o máximo
		go uploadFile(files[0].Name(), uploadControl, errorFileUpload)
	}
	wg.Wait()
}

func uploadFile(filename string, uploadControl <-chan struct{}, errorFileUpload chan<- string) {
	completeFileName := fmt.Sprintf("./tmp/%s", filename)
	fmt.Printf("Uploading file %s to bucket %s", completeFileName, s3Bucket)
	file, err := os.Open(completeFileName)
	if err != nil {
		fmt.Printf("Error opening file %s\n", completeFileName)
		<-uploadControl                     // "libera" um slot do canal
		errorFileUpload <- completeFileName // se houver erro, adiciono dentro do canal de error
		return
	}
	defer file.Close()

	_, err = s3Client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(filename),
		Body:   file,
	})
	if err != nil {
		fmt.Printf("Error uploading file %s\n", completeFileName)
		<-uploadControl                     // "libera" um slot do canal
		errorFileUpload <- completeFileName // se houver erro, adiciono dentro do canal de error
		return
	}

	fmt.Printf("File %s uploaded successfuly\n", completeFileName)
	<-uploadControl // "libera" um slot do canal
}
