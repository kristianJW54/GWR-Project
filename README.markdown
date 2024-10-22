# GWR Project 

**Track every Great Western Railway train across the UK in near realtime.**

---

### Summary

Welcome to the GWR Project! This Go-based backend application is designed for train enthusiasts and data aficionados alike.
It provides near-real-time data for all Great Western Railway (GWR) trains currently operating across the UK.

---

### Features

- Real-Time Data Caching: Utilizes Redis to cache live data on GWR trains, ensuring quick access and efficient data retrieval.
- Message Channels: Data is published to message channels, facilitating communication with Server-Sent Events (SSE) servers for live updates.
- REST API: A robust RESTful API allows front-end applications to easily access and utilize the cached data, enabling developers to create innovative applications showcasing GWR trains and routes.

---

### How it works

The initial data is requested from the NationalRail Data Feed https://www.nationalrail.co.uk/developers/darwin-data-feeds/
which is a SOAP API using a Token and Station Code.

Every service from each of the 60 major GWR stations are concurrently requested and processed into individual routes as JSON Objects.
Each route is checked before being cached in Redis, any changes in current cached data is sent via channels and published for the Server Sent Event servers to pick up.

A polling service then polls the NationalRail data at defined intervals processing, caching and publishing to keep a near realtime view on every GWR train active

The polling service is then containerised in docker and run separately alongside Redis. A custom SSE Server is run as its own service which tracks client connections
and broadcasts subscribed messages to the clients and their respective channels. The SSE Server is also containerised and run separately for scalability and load.

---

### Goals of the Project

The Main goals of the project are to:

- Build a robust and scalable backend
- Enable frontends to be built by defining a REST API to be used
- To work with SOAP APIs
- To work with messaging systems like pub/sub
- Collaborate with other developers
- TRAINS! I love the Great Western Railway :)

---

### Frontend Challenge

**! Coming soon !**

Once this project is complete, a detailed REST API and HOWTO will be published in order for others to utilize the data
and build awesome frontends

---

### To Run the Project

1. Sign up for a token at https://realtime.nationalrail.co.uk/OpenLDBWSRegistration/


2. Clone the repo
````bash
git clone https://github.com/kristianJW54/GWR-Project.git
````

3. Install dependencies

````bash
go mod tidy
````

4. Replace Token and URL
````go
// Replace this
Token := os.Getenv("TRAIN_TOKEN")
URL := os.Getenv("URL")
if Token == "" || URL == "" {
log.Fatal("environment variable is not set")
}

// With this
Token := "[Insert your token from step 1]"
URL := "https://lite.realtime.nationalrail.co.uk/OpenLDBWS/ldb12.asmx"
````

5. Run Redis Locally in Docker Container
````bash
docker run --name gwr-redis -p 6379:6379 -d redis
````

---

### Contributing

**Contributions are welcome! If you have ideas for improvements or features, feel free to submit a pull request or open an issue.**







