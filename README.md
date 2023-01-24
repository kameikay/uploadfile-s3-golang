Para subir arquivos no bucket da S3, devemos:

1) Exemplo de gerador de arquivos tmp (./cmd/generator/main.go)

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	i := 0

	for {
		file, err := os.Create(fmt.Sprintf("./tmp/file-%d.txt", i))
		if err != nil {
			panic(err)
		}

		defer file.Close()
		file.WriteString("Hello")
		i++

		if i == 10 {
			return
		}
	}
}
```

2) Cria um uploader (onde estará a ponte com a S3) - Método mais simples, sem multithreading:

```go
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

var (
	s3Client *s3.S3
)

var s3Bucket = "kameikay-goexpert-bucket-example"

func init() {
	sess, err := session.NewSession(
		&aws.Config{
			Region: aws.String("sa-east-1"), // Verificar se é o mesmo que está setado na AWS
			Credentials: credentials.NewStaticCredentials(
				"AKIA26VCOH6JKTDDYPHB",                     // Access key ID
				"FP96HP2Fp5WCGf0z0UqwC4mv9XtMeS99kqEGa7pf", // Secret access key
				"",
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

	for {
		files, err := dir.ReadDir(1)
		if err != nil {
			if err == io.EOF { // end of file
				break
			}
			fmt.Printf("Error reading directory: %s\n", err)
			continue
		}
		uploadFile(files[0].Name())
	}
}

func uploadFile(filename string) {
	completeFileName := fmt.Sprintf("./tmp/%s", filename)
	fmt.Printf("Uploading file %s to bucket %s", completeFileName, s3Bucket)
	file, err := os.Open(completeFileName)
	if err != nil {
		fmt.Printf("Error opening file %s\n", completeFileName)
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
		return
	}

	fmt.Printf("File %s uploaded successfuly\n", completeFileName)
}
```

3) Ir para aws.amazon.com, logar e:

1. IAM
2. Usuários
3. Criar usuário e pegar o Access Key ID e Secret Access Key
4. S3
5. Criar bucket

4)  Para fazer de forma paralela:

- Se só adicionássemos waitgroups colocando wg.Add(1) antes da go function e um defer wg.Done() dentro da função de upload, poderia dar gargalo no sistema, na AWS, etc.
- Para resolver, devemos limitar a quantidade de threads com channels:
    1. Criar um channel que receberá uma struct vazia, limitando a quantidade de recebimento
    
    ```go
    uploadControl := make(chan struct{}, 100) // limita a quantidade em 100, neste exemplo
    ```
    
    b.  Antes da go routine, adicionar a struct vazia para que vá "enchendo” o channel até o limite estipulado (100, no caso). Quando encher, não irá executar a função, evitando que ocorra os gargalos.
    
    ```go
    uploadControl <- struct{}{} // Vai adicionando uma struct vazia até encher o channel. Quando encher, não vai
    		// permitir que execute a go routine até que seja menor que o máximo
    ```
    
    c. Dentro da função de upload, retirar (liberar) um slot a cada execução ou erro.
    
    ```go
    <-uploadControl // "libera" um slot do canal
    ```
    
- E se houver um erro dentre os arquivos? Preciso saber quem falhou para não haver perda de arquivos.
    - Criamos um novo channel que vai receber o nome dos arquivos que deram erro.
    
    ```go
    errorFileUpload := make(chan string, 10)  // Crio um canal com os nomes dos arquivos que deram erro
    ```
    
    - Fazemos uma nova go routine que vai ficar verificando se há arquivos dentro do channel de erros. Se sim, vai colocar uma struct vazia no uploadControl, vai adicionar um waitGroup para que a thread não “morra” e vai chamar a go routine de upload novamente, tentando fazer o upload de novo.
    
    ```go
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
    ```
    
    - Dentro da função de upload, vai adicionar no channel o nome do arquivo, sempre que houver erro:
    
    ```go
    if err != nil {
    	fmt.Printf("Error opening file %s\n", completeFileName)
    	<-uploadControl                     // "libera" um slot do canal
    	errorFileUpload <- completeFileName // se houver erro, adiciono dentro do canal de error
    	return
    }
    // ...
    
    if err != nil {
    	fmt.Printf("Error uploading file %s\n", completeFileName)
    	<-uploadControl                     // "libera" um slot do canal
    	errorFileUpload <- completeFileName // se houver erro, adiciono dentro do canal de error
    	return
    }
    
    ```
    
    - Perceba que a função upload deve receber mais um argumento: o channel de erro (errorFileUpload chan← string)