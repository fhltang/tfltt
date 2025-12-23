# tfltt

A web application to render Tube Timetables.

An experiment in Vibe Coding in Antigravity.

## Configuration

1. Get a TFL API Key from [https://api-portal.tfl.gov.uk/](https://api-portal.tfl.gov.uk/).
2. Create `app_key.txt` in the root directory.
3. Paste your API key into `app_key.txt`.

## Running

```bash
go run .
```

## Regeneration

To regenerate the TFL API client (e.g., after updating `tfl_swagger.json`):

1. **Fetch the Swagger Spec**:
   Download the latest specification from TFL:
   ```bash
   curl -o tfl_swagger.json https://api.tfl.gov.uk/swagger/docs/v1
   ```

2. **Generate Client**:
   Install `go-swagger` (see [installation instructions](https://github.com/go-openapi/go-swagger/blob/master/docs/install.md)).
   Run `go-swagger`:
   ```bash
   swagger generate client -f tfl_swagger.json -t tfl --skip-validation
   ```

## Deployment

You can deploy this application directly to Google Cloud Run from source. This will automatically use the `Dockerfile` to build and deploy the container.

### Deploy from Source
```bash
gcloud run deploy tfltt \
  --source . \
  --platform managed \
  --region europe-west1 \
  --allow-unauthenticated \
  --set-env-vars TFL_APP_KEY=your_key_here
```

Replace `your_key_here` with your TfL API key. The application will listen on the `PORT` environment variable provided by Cloud Run (defaulting to 8080).
