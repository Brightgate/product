pipeline {
    agent any
    triggers {
        cron('@daily')
    }
    stages {
        stage('sysroot') {
            environment {
                GOARCH = 'arm'
                GOARM = '7'
                KEY_SYSROOT_UPLOADER = credentials('sysroot-uploader')
            }
            steps {
                sh 'make upload-sysroot'
            }
        }
    }
}
