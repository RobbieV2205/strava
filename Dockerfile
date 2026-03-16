FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py database.py strava_api.py ./

CMD ["python", "main.py"]

# docker build -t strava-sync

# docker run -d --name strava-sync 
#  -v /pad/naar/strava/.env:/app/.env:ro \
#  -v /pad/naar/strava/strava_tokens.json:/app/strava_tokens.json strava-sync